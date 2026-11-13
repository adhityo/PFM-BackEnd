package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/model"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/storage"
	"pfm-backend/internal/repository"
)

// FileService handles uploads, access control and cleanup.
type FileService struct {
	tx         *database.TxManager
	repo       repository.FileObjectRepository
	images     storage.ImageStorage
	docs       storage.DocumentStorage
	audit      *audit.Recorder
	log        *slog.Logger
	presignTTL time.Duration
	now        func() time.Time
}

func NewFileService(tx *database.TxManager, repo repository.FileObjectRepository, images storage.ImageStorage, docs storage.DocumentStorage,
	rec *audit.Recorder, log *slog.Logger, presignTTL time.Duration) *FileService {
	return &FileService{tx: tx, repo: repo, images: images, docs: docs, audit: rec, log: log, presignTTL: presignTTL, now: time.Now}
}

// Upload validates, stores and registers a file as pending.
func (s *FileService) Upload(ctx context.Context, category, purpose, originalName string, r io.Reader) (model.FileObject, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return model.FileObject{}, err
	}
	policy, ok := UploadPolicies[purpose]
	if !ok || policy.Category != category {
		return model.FileObject{}, apperr.Invalid("purpose", "oneof", "purpose tidak dikenal untuk kategori "+category)
	}
	raw, err := io.ReadAll(io.LimitReader(r, policy.MaxBytes+1))
	if err != nil {
		return model.FileObject{}, apperr.BadRequest("file tidak dapat dibaca", err)
	}
	p, err := prepareUpload(raw, policy)
	if err != nil {
		return model.FileObject{}, err
	}

	id := uuid.Must(uuid.NewV7())
	now := s.now().UTC()
	params := sqlc.InsertFileObjectParams{Uuid: id, Category: category, OriginalName: sanitizeFileName(originalName),
		MimeType: p.mime, SizeBytes: int64(len(p.data)), ChecksumSha256: p.checksum, Actor: ptr(actor.AuditName())}

	// Storage first, database second; the DB row is the source of truth and
	// orphans are removed by CleanupStale (PRD §8.2).
	if category == model.FileCategoryImage {
		params.Storage = "local"
		params.ObjectKey = fmt.Sprintf("images/%s/%04d/%02d/%s.%s", purposeDir(purpose), now.Year(), now.Month(), id, p.ext)
		if err := s.images.Put(ctx, params.ObjectKey, bytes.NewReader(p.data)); err != nil {
			return model.FileObject{}, fmt.Errorf("store image: %w", err)
		}
	} else {
		params.Storage = "s3"
		params.Bucket = ptr(s.docs.Bucket())
		params.ObjectKey = fmt.Sprintf("documents/%s/%04d/%02d/%s.%s", purposeDir(purpose), now.Year(), now.Month(), id, p.ext)
		if err := s.docs.Put(ctx, params.ObjectKey, bytes.NewReader(p.data), int64(len(p.data)), p.mime, p.checksum); err != nil {
			return model.FileObject{}, fmt.Errorf("store document: %w", err)
		}
	}

	var obj model.FileObject
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		obj, err = s.repo.Insert(ctx, params)
		if err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "file_object.upload", Entity: "file_object", EntityUUID: id,
			Description: "purpose=" + purpose + " mime=" + p.mime, Route: "POST /files/" + category + "s"})
	})
	if err != nil {
		s.removeObject(context.WithoutCancel(ctx), model.FileObject{Storage: params.Storage, ObjectKey: params.ObjectKey})
		return model.FileObject{}, err
	}
	return obj, nil
}

// canAccess: the uploader, or anyone holding perm.
func canAccessFile(actor auth.Actor, o model.FileObject, perm string) bool {
	return (o.CreatedBy != nil && *o.CreatedBy == actor.AuditName()) || actor.Can(perm)
}

// Get returns metadata the actor may see. Inaccessible files are reported as 404.
func (s *FileService) Get(ctx context.Context, id uuid.UUID) (model.FileObject, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return model.FileObject{}, err
	}
	o, err := s.repo.GetByUUID(ctx, id)
	if err != nil {
		return model.FileObject{}, err
	}
	if !canAccessFile(actor, o, permission.FileRead) || o.Status == "quarantined" {
		return model.FileObject{}, apperr.ErrNotFound
	}
	return o, nil
}

// OpenImage streams a local image.
func (s *FileService) OpenImage(ctx context.Context, id uuid.UUID) (model.FileObject, io.ReadCloser, error) {
	o, err := s.Get(ctx, id)
	if err != nil {
		return model.FileObject{}, nil, err
	}
	if o.Category != model.FileCategoryImage {
		return model.FileObject{}, nil, apperr.NotFound("file bukan gambar; gunakan /download")
	}
	rc, err := s.images.Open(ctx, o.ObjectKey)
	if err != nil {
		return model.FileObject{}, nil, fmt.Errorf("open image: %w", err)
	}
	return o, rc, nil
}

// DownloadURL returns a short-lived presigned URL for a document and audits the access.
func (s *FileService) DownloadURL(ctx context.Context, id uuid.UUID) (string, error) {
	o, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if o.Category != model.FileCategoryDocument {
		return "", apperr.NotFound("file bukan dokumen; gunakan /content")
	}
	url, err := s.docs.PresignGet(ctx, o.ObjectKey, o.OriginalName, s.presignTTL)
	if err != nil {
		return "", err
	}
	if err := s.audit.Record(ctx, audit.Entry{Action: "file_object.download", Entity: "file_object", EntityUUID: id,
		Route: "GET /files/{uuid}/download"}); err != nil {
		return "", err
	}
	return url, nil
}

// Delete removes a pending file. Active files belong to an entity and are
// replaced or removed through that entity.
func (s *FileService) Delete(ctx context.Context, id uuid.UUID) error {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return err
	}
	var obj model.FileObject
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		o, err := s.repo.GetByUUIDForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if !canAccessFile(actor, o, permission.FileDelete) {
			return apperr.ErrNotFound
		}
		if o.Status == "active" {
			return apperr.Conflict("file sudah dipakai oleh data lain")
		}
		if obj, err = s.repo.SoftDelete(ctx, id, actor.AuditName()); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "file_object.delete", Entity: "file_object", EntityUUID: id,
			Route: "DELETE /files/{uuid}"})
	})
	if err != nil {
		return err
	}
	s.removeObject(ctx, obj)
	return nil
}

// Attach marks a pending upload as used by an entity field. It must be called
// inside the entity's transaction and returns the file_object id for the FK.
func (s *FileService) Attach(ctx context.Context, fileUUID uuid.UUID, purpose, field string) (int64, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return 0, err
	}
	if !database.InTx(ctx) {
		return 0, errors.New("file.Attach must run inside a transaction")
	}
	o, err := s.repo.GetByUUIDForUpdate(ctx, fileUUID)
	if err != nil {
		if errors.Is(err, apperr.ErrNotFound) {
			return 0, apperr.Invalid(field, "exists", "file tidak ditemukan")
		}
		return 0, err
	}
	switch {
	case o.CreatedBy == nil || *o.CreatedBy != actor.AuditName():
		return 0, apperr.Invalid(field, "owner", "file harus diunggah oleh pengguna yang sama")
	case o.Status != "pending":
		return 0, apperr.Invalid(field, "pending", "file sudah dipakai atau tidak valid")
	case !KeyMatchesPurpose(o.ObjectKey, purpose):
		return 0, apperr.Invalid(field, "purpose", "file diunggah untuk keperluan lain")
	}
	if err := s.repo.Activate(ctx, o.ID, actor.AuditName()); err != nil {
		return 0, err
	}
	return o.ID, nil
}

// CleanupStale deletes pending uploads older than before (worker job).
func (s *FileService) CleanupStale(ctx context.Context, before time.Time) (int, error) {
	n := 0
	for {
		objs, err := s.repo.ListStalePending(ctx, before, 100)
		if err != nil {
			return n, err
		}
		if len(objs) == 0 {
			return n, nil
		}
		for _, o := range objs {
			s.removeObject(ctx, o)
			if err := s.repo.HardDelete(ctx, o.ID); err != nil {
				return n, err
			}
			n++
		}
	}
}

// removeObject deletes the stored bytes; failures are logged, not returned,
// because the database state is already final.
func (s *FileService) removeObject(ctx context.Context, o model.FileObject) {
	var err error
	switch o.Storage {
	case "local":
		err = s.images.Delete(ctx, o.ObjectKey)
	case "s3":
		err = s.docs.Delete(ctx, o.ObjectKey)
	}
	if err != nil {
		s.log.Warn("remove stored object failed", "key", strings.TrimSpace(o.ObjectKey), "err", err)
	}
}
