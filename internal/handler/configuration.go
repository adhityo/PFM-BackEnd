package handler

import (
	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/dto"
	"pfm-backend/internal/permission"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

// ConfigurationHandler serves the /configuration singleton (GET and PUT only).
type ConfigurationHandler struct {
	v   httpx.Validator
	svc *service.ConfigurationService
}

func NewConfigurationHandler(v httpx.Validator, svc *service.ConfigurationService) *ConfigurationHandler {
	return &ConfigurationHandler{v: v, svc: svc}
}

func (h *ConfigurationHandler) Register(r *auth.Router) {
	r.Get("/configuration", permission.ConfigurationRead, h.Get)
	r.Put("/configuration", permission.ConfigurationUpdate, h.Update)
}

func (h *ConfigurationHandler) Get(c fiber.Ctx) error {
	row, err := h.svc.Get(c.Context())
	if err != nil {
		return err
	}
	httpx.SetETag(c, row.UpdatedAt)
	return httpx.OK(c, dto.ConfigurationFromRow(row))
}

func (h *ConfigurationHandler) Update(c fiber.Ctx) error {
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req dto.ConfigurationRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	row, err := h.svc.Update(c.Context(), req, expected)
	if err != nil {
		return err
	}
	httpx.SetETag(c, row.UpdatedAt)
	return httpx.OK(c, dto.ConfigurationFromRow(row))
}
