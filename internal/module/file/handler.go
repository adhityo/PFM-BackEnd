package file

import (
	"mime"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
)

// Response is the public view of a file_object.
type Response struct {
	UUID           uuid.UUID `json:"uuid"`
	Category       string    `json:"category"`
	OriginalName   string    `json:"original_name"`
	MimeType       string    `json:"mime_type"`
	SizeBytes      int64     `json:"size_bytes"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

func toResponse(o Object) Response {
	return Response{UUID: o.Uuid, Category: o.Category, OriginalName: o.OriginalName, MimeType: o.MimeType,
		SizeBytes: o.SizeBytes, ChecksumSHA256: o.ChecksumSha256, Status: o.Status, CreatedAt: o.CreatedAt}
}

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) upload(category string) fiber.Handler {
	return func(c fiber.Ctx) error {
		fh, err := c.FormFile("file")
		if err != nil {
			return apperr.Invalid("file", "required", "field multipart 'file' wajib diisi")
		}
		purpose := c.FormValue("purpose")
		if fh.Size > MaxUploadBytes {
			return apperr.PayloadTooLarge("ukuran file melebihi batas")
		}
		f, err := fh.Open()
		if err != nil {
			return apperr.BadRequest("file tidak dapat dibaca", err)
		}
		defer func() { _ = f.Close() }()
		o, err := h.svc.Upload(c.Context(), category, purpose, fh.Filename, f)
		if err != nil {
			return err
		}
		return httpx.Created(c, "/api/v1/files/"+o.Uuid.String(), toResponse(o))
	}
}

func (h *Handler) Get(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	o, err := h.svc.Get(c.Context(), id)
	if err != nil {
		return err
	}
	return httpx.OK(c, toResponse(o))
}

func (h *Handler) Content(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	o, rc, err := h.svc.OpenImage(c.Context(), id)
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

func (h *Handler) Download(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	url, err := h.svc.DownloadURL(c.Context(), id)
	if err != nil {
		return err
	}
	c.Set(fiber.HeaderCacheControl, "no-store")
	return c.Redirect().Status(fiber.StatusFound).To(url)
}

func (h *Handler) Delete(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	if err := h.svc.Delete(c.Context(), id); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

// Routes registers /files endpoints. Reading and deleting check ownership or
// permission inside the service, so only an authenticated session is required.
func (h *Handler) Routes(r *auth.Router) {
	f := r.Group("/files")
	f.Post("/images", PermCreate, h.upload(CategoryImage))
	f.Post("/documents", PermCreate, h.upload(CategoryDocument))
	f.Authenticated(fiber.MethodGet, "/:uuid", h.Get)
	f.Authenticated(fiber.MethodGet, "/:uuid/content", h.Content)
	f.Authenticated(fiber.MethodGet, "/:uuid/download", h.Download)
	f.Authenticated(fiber.MethodDelete, "/:uuid", h.Delete)
}
