package crud

import (
	"strings"

	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/httpx"
)

type handler[Row, Req, Resp any] struct {
	svc *service[Row, Req, Resp]
	v   httpx.Validator
}

func (h *handler[Row, Req, Resp]) parseList(c fiber.Ctx) (httpx.ListQuery, error) {
	r := h.svc.res
	return httpx.ParseListQuery(c, sortOptions(r.Sorts), r.DefaultSort, r.Filters...)
}

func (h *handler[Row, Req, Resp]) respondList(c fiber.Ctx, q httpx.ListQuery) error {
	items, total, err := h.svc.list(c.Context(), q)
	if err != nil {
		return err
	}
	return httpx.List(c, items, q.Page, total)
}

func (h *handler[Row, Req, Resp]) list(c fiber.Ctx) error {
	q, err := h.parseList(c)
	if err != nil {
		return err
	}
	return h.respondList(c, q)
}

// nestedList serves GET /<parents>/{parent_uuid}/<children>.
func (h *handler[Row, Req, Resp]) nestedList(filter string) fiber.Handler {
	return func(c fiber.Ctx) error {
		parent, err := httpx.UUIDParam(c, "parent_uuid")
		if err != nil {
			return err
		}
		q, err := h.parseList(c)
		if err != nil {
			return err
		}
		q.Filters[filter] = parent.String()
		return h.respondList(c, q)
	}
}

func (h *handler[Row, Req, Resp]) write(c fiber.Ctx, status int, row Row) error {
	meta := h.svc.res.Store.Meta(row)
	httpx.SetETag(c, meta.UpdatedAt)
	if status == fiber.StatusCreated {
		return httpx.Created(c, "/api/v1"+h.svc.res.Path+"/"+meta.UUID.String(), h.svc.res.Response(row))
	}
	return httpx.OK(c, h.svc.res.Response(row))
}

func (h *handler[Row, Req, Resp]) get(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	row, err := h.svc.get(c.Context(), id)
	if err != nil {
		return err
	}
	return h.write(c, fiber.StatusOK, row)
}

func (h *handler[Row, Req, Resp]) create(c fiber.Ctx) error {
	var req Req
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	row, err := h.svc.create(c.Context(), req)
	if err != nil {
		return err
	}
	return h.write(c, fiber.StatusCreated, row)
}

// importCSV accepts text/csv whose header row uses the JSON field names.
func (h *handler[Row, Req, Resp]) importCSV(c fiber.Ctx) error {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(c.Get(fiber.HeaderContentType), ";", 2)[0]))
	if ct != "text/csv" {
		return apperr.UnsupportedMediaType("Content-Type harus text/csv")
	}
	reqs, err := parseCSV[Req](c.Body(), h.v)
	if err != nil {
		return err
	}
	n, err := h.svc.importRows(c.Context(), reqs)
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": fiber.Map{"imported": n},
		"meta": httpx.Meta{RequestID: httpx.RequestID(c)}})
}

func (h *handler[Row, Req, Resp]) update(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	var req Req
	if err := httpx.BindJSON(c, h.v, &req); err != nil {
		return err
	}
	row, err := h.svc.update(c.Context(), id, req, expected)
	if err != nil {
		return err
	}
	return h.write(c, fiber.StatusOK, row)
}

func (h *handler[Row, Req, Resp]) delete(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	expected, err := httpx.IfMatch(c)
	if err != nil {
		return err
	}
	if err := h.svc.delete(c.Context(), id, expected); err != nil {
		return err
	}
	return httpx.NoContent(c)
}

// readHandler serves ReadResource routes.
type readHandler[Row, Resp any] struct {
	res ReadResource[Row, Resp]
}

func (h *readHandler[Row, Resp]) parseList(c fiber.Ctx) (httpx.ListQuery, error) {
	return httpx.ParseListQuery(c, sortOptions(h.res.Sorts), h.res.DefaultSort, h.res.Filters...)
}

func (h *readHandler[Row, Resp]) respond(c fiber.Ctx, q httpx.ListQuery) error {
	items, total, err := listRows(c.Context(), h.res.Store, h.res.Response, q)
	if err != nil {
		return err
	}
	return httpx.List(c, items, q.Page, total)
}

func (h *readHandler[Row, Resp]) list(c fiber.Ctx) error {
	q, err := h.parseList(c)
	if err != nil {
		return err
	}
	return h.respond(c, q)
}

func (h *readHandler[Row, Resp]) nestedList(filter string) fiber.Handler {
	return func(c fiber.Ctx) error {
		parent, err := httpx.UUIDParam(c, "parent_uuid")
		if err != nil {
			return err
		}
		q, err := h.parseList(c)
		if err != nil {
			return err
		}
		q.Filters[filter] = parent.String()
		return h.respond(c, q)
	}
}

func (h *readHandler[Row, Resp]) get(c fiber.Ctx) error {
	id, err := httpx.UUIDParam(c, "uuid")
	if err != nil {
		return err
	}
	row, err := h.res.Store.Get(c.Context(), id)
	if err != nil {
		return err
	}
	httpx.SetETag(c, h.res.Store.Meta(row).UpdatedAt)
	return httpx.OK(c, h.res.Response(row))
}
