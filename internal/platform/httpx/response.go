// Package httpx contains HTTP plumbing shared by all modules: envelopes,
// error mapping, strict binding, ETags, pagination and middleware.
package httpx

import (
	"github.com/gofiber/fiber/v3"
)

// Meta is attached to every response.
type Meta struct {
	RequestID  string `json:"request_id"`
	Page       int    `json:"page,omitempty"`
	PerPage    int    `json:"per_page,omitempty"`
	Total      int64  `json:"total,omitempty"`
	TotalPages int64  `json:"total_pages,omitempty"`
}

type envelope struct {
	Data any  `json:"data"`
	Meta Meta `json:"meta"`
}

// OK writes 200 with {"data": v}.
func OK(c fiber.Ctx, v any) error {
	return c.Status(fiber.StatusOK).JSON(envelope{Data: v, Meta: Meta{RequestID: RequestID(c)}})
}

// Created writes 201 with Location and {"data": v}.
func Created(c fiber.Ctx, location string, v any) error {
	c.Location(location)
	return c.Status(fiber.StatusCreated).JSON(envelope{Data: v, Meta: Meta{RequestID: RequestID(c)}})
}

// List writes 200 with pagination metadata. items must be a non-nil slice.
func List(c fiber.Ctx, items any, p Page, total int64) error {
	pages := int64(0)
	if p.PerPage > 0 {
		pages = (total + int64(p.PerPage) - 1) / int64(p.PerPage)
	}
	return c.Status(fiber.StatusOK).JSON(envelope{Data: items, Meta: Meta{
		RequestID: RequestID(c), Page: p.Page, PerPage: p.PerPage, Total: total, TotalPages: pages,
	}})
}

// NoContent writes 204.
func NoContent(c fiber.Ctx) error {
	return c.SendStatus(fiber.StatusNoContent)
}
