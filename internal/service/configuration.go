package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/audit"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/repository"
)

const qrLogoPurpose = "configuration.qr_logo"

// ConfigurationService manages the configuration singleton (PRD §10.5).
type ConfigurationService struct {
	tx    *database.TxManager
	repo  repository.ConfigurationRepository
	files *FileService
	audit *audit.Recorder
}

func NewConfigurationService(tx *database.TxManager, repo repository.ConfigurationRepository, files *FileService,
	rec *audit.Recorder) *ConfigurationService {
	return &ConfigurationService{tx: tx, repo: repo, files: files, audit: rec}
}

func (s *ConfigurationService) Get(ctx context.Context) (sqlc.GetConfigurationRow, error) {
	return s.repo.Get(ctx)
}

// Update replaces the editable fields; counters are never touched.
func (s *ConfigurationService) Update(ctx context.Context, req dto.ConfigurationRequest, expected time.Time) (sqlc.GetConfigurationRow, error) {
	actor, err := auth.MustActor(ctx)
	if err != nil {
		return sqlc.GetConfigurationRow{}, err
	}
	var out sqlc.GetConfigurationRow
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		id, updatedAt, err := s.repo.Lock(ctx)
		if err != nil {
			return err
		}
		if !updatedAt.Equal(expected) {
			return apperr.ErrPreconditionFailed
		}
		cur, err := s.repo.Get(ctx)
		if err != nil {
			return err
		}
		logoID, err := s.resolveLogo(ctx, req.QRLogoFileUUID, cur)
		if err != nil {
			return err
		}
		if err := s.repo.Update(ctx, sqlc.UpdateConfigurationParams{
			SystemUsageType: req.SystemUsageType, CompanyName: crud.Text(req.CompanyName),
			CompanyAddress: crud.Text(req.CompanyAddress), DefaultProrateDays: req.DefaultProrateDays,
			AgentNumberTemplate: crud.Text(req.AgentNumberTemplate), CertificateNumberTemplate: crud.Text(req.CertificateNumberTemplate),
			InvestorNumberTemplate: crud.Text(req.InvestorNumberTemplate), EmitentCode: crud.Text(req.EmitentCode),
			ProxyApiUrl: crud.Text(req.ProxyAPIURL), IsUsingQrLogo: req.IsUsingQRLogo, QrLogoFileID: logoID,
			IsUsingAgreement: req.IsUsingAgreement, Actor: ptr(actor.AuditName()), ID: id, ExpectedUpdatedAt: expected,
		}); err != nil {
			return err
		}
		if out, err = s.repo.Get(ctx); err != nil {
			return err
		}
		return s.audit.Record(ctx, audit.Entry{Action: "configuration.update", Entity: "configuration",
			EntityUUID: out.Uuid, Route: "PUT /configuration"})
	})
	return out, err
}

// resolveLogo keeps the current logo, clears it, or attaches a new pending upload.
func (s *ConfigurationService) resolveLogo(ctx context.Context, raw string, cur sqlc.GetConfigurationRow) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, apperr.Invalid("qr_logo_file_uuid", "uuid", "harus berupa UUID yang valid")
	}
	if cur.QrLogoFileUuid != nil && *cur.QrLogoFileUuid == id {
		return cur.QrLogoFileID, nil
	}
	fid, err := s.files.Attach(ctx, id, qrLogoPurpose, "qr_logo_file_uuid")
	if err != nil {
		return nil, err
	}
	return &fid, nil
}
