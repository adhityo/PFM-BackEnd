package handler

import (
	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/dto"
	"pfm-backend/internal/platform/auth"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

// AuthHandler serves /auth and /me.
type AuthHandler struct {
	v   httpx.Validator
	svc *service.AuthService
}

func NewAuthHandler(v httpx.Validator, svc *service.AuthService) *AuthHandler {
	return &AuthHandler{v: v, svc: svc}
}

// Register mounts the routes. limiter guards the unauthenticated credential endpoints.
func (h *AuthHandler) Register(r *auth.Router, limiter fiber.Handler) {
	a := r.Group("/auth")
	a.Public(fiber.MethodPost, "/login", limiter, h.Login)
	a.Public(fiber.MethodPost, "/refresh", limiter, h.Refresh)
	a.Public(fiber.MethodPost, "/logout", limiter, h.Logout)
	a.Public(fiber.MethodPost, "/activate", limiter, h.Activate)
	a.Public(fiber.MethodPost, "/forgot-password", limiter, h.ForgotPassword)
	a.Authenticated(fiber.MethodPost, "/logout-all", h.LogoutAll)
	a.Authenticated(fiber.MethodPost, "/change-password", h.ChangePassword)
	r.Authenticated(fiber.MethodGet, "/me", h.Me)
}

func (h *AuthHandler) Login(c fiber.Ctx) error {
	var req dto.LoginRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	res, err := h.svc.Login(c.Context(), req, clientInfo(c))
	if err != nil {
		return err
	}
	return httpx.OK(c, res)
}

func (h *AuthHandler) Refresh(c fiber.Ctx) error {
	var req dto.RefreshRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	res, err := h.svc.Refresh(c.Context(), req.RefreshToken, clientInfo(c))
	if err != nil {
		return err
	}
	return httpx.OK(c, res)
}

func (h *AuthHandler) Logout(c fiber.Ctx) error {
	var req dto.RefreshRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	if err := h.svc.Logout(c.Context(), req.RefreshToken); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

func (h *AuthHandler) LogoutAll(c fiber.Ctx) error {
	if err := h.svc.LogoutAll(c.Context()); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

// ForgotPassword always answers 204, whether the email is registered or not.
func (h *AuthHandler) ForgotPassword(c fiber.Ctx) error {
	var req dto.ForgotPasswordRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	if err := h.svc.ForgotPassword(c.Context(), req, clientInfo(c)); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

func (h *AuthHandler) Activate(c fiber.Ctx) error {
	var req dto.ActivateRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	if err := h.svc.Activate(c.Context(), req, clientInfo(c)); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

func (h *AuthHandler) ChangePassword(c fiber.Ctx) error {
	var req dto.ChangePasswordRequest
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	if err := h.svc.ChangePassword(c.Context(), req); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

func (h *AuthHandler) Me(c fiber.Ctx) error {
	res, err := h.svc.Me(c.Context())
	if err != nil {
		return err
	}
	return httpx.OK(c, res)
}
