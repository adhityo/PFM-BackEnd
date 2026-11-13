package handler

import (
	"mime"

	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/dto"
	"pfm-backend/internal/model"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

// FileHandler serves /files.
type FileHandler struct {
	files *service.FileService
}

func NewFileHandler(files *service.FileService) *FileHandler { return &FileHandler{files: files} }

// Register mounts /files. Reading and deleting check ownership or permission
// inside the service, so those routes only need an authenticated session.
func (h *FileHandler) Register(r *auth.Router) {
	g := r.Group("/files")
	g.Post("/images", permission.FileCreate, h.upload(model.FileCategoryImage))
	g.Post("/documents", permission.FileCreate, h.upload(model.FileCategoryDocument))
	g.Authenticated(fiber.MethodGet, "/:uuid", h.Get)
	g.Authenticated(fiber.MethodGet, "/:uuid/content", h.Content)
	g.Authenticated(fiber.MethodGet, "/:uuid/download", h.Download)
	g.Authenticated(fiber.MethodDelete, "/:uuid", h.Delete)
}

func (h *FileHandler) upload(category string) fiber.Handler {
	return func(c fiber.Ctx) error {
		fh, err := c.FormFile("file")
		if err != nil {
			return apperr.Invalid("file", "required", "field multipart 'file' wajib diisi")
		}
		if fh.Size > service.MaxUploadBytes {
			return apperr.PayloadTooLarge("ukuran file melebihi batas")
		}
		f, err := fh.Open()
		if err != nil {
			return apperr.BadRequest("file tidak dapat dibaca", err)
		}
		defer func() { _ = f.Close() }()
		o, err := h.files.Upload(c.Context(), category, c.FormValue("purpose"), fh.Filename, f)
		if err != nil {
			return err
		}
		return httpx.Created(c, "/api/v1/files/"+o.Uuid.String(), dto.FileFromModel(o))
	}
}

func (h *FileHandler) Get(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	o, err := h.files.Get(c.Context(), id)
	if err != nil {
		return err
	}
	return httpx.OK(c, dto.FileFromModel(o))
}

func (h *FileHandler) Content(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	o, rc, err := h.files.OpenImage(c.Context(), id)
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderContentType, o.MimeType)
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	c.Set(fiber.HeaderContentSecurityPolicy, "default-src 'none'; img-src 'self'; style-src 'unsafe-inline'")
	c.Set(fiber.HeaderContentDisposition, mime.FormatMediaType("inline", map[string]string{"filename": o.OriginalName}))
	c.Set(fiber.HeaderCacheControl, "private, max-age=300")
	return c.SendStream(rc, int(o.SizeBytes))
}

func (h *FileHandler) Download(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	url, err := h.files.DownloadURL(c.Context(), id)
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Redirect().Status(fiber.StatusFound).To(url)
}

func (h *FileHandler) Delete(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	if err := h.files.Delete(c.Context(), id); err != nil {
		return err
	}
	return httpx.NoContent(c)
}
