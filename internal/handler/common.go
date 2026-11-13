// Package handler adapts HTTP requests to services: binding, validation,
// ETags and response envelopes. No business rules and no SQL live here.
package handler

import (
	"net/netip"

	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/model"
	"pfm-backend/internal/platform/httpx"
	"pfm-backend/internal/service"
)

func clientInfo(c fiber.Ctx) service.ClientInfo {
	ci := service.ClientInfo{UserAgent: c.Get(fiber.HeaderUserAgent), RequestID: httpx.RequestID(c)}
	if len(ci.UserAgent) > 255 {
		ci.UserAgent = ci.UserAgent[:255]
	}
	if ip, err := netip.ParseAddr(c.IP()); err == nil {
		ci.IP = &ip
	}
	return ci
}

func paging(q httpx.ListQuery) model.Paging {
	return model.Paging{Sort: q.Sort, Limit: q.Page.Limit(), Offset: q.Page.Offset()}
}
