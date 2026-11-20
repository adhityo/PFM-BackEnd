package crud

// ResourceDoc describes a resource for API documentation (cmd/apidoc).
type ResourceDoc struct {
	Table, Label, Module, Path string
	ReadOnly                   bool
	Import                     bool
	Sorts                      []string
	DefaultSort                string
	Filters                    []string
	Nested                     []Nested
	Files                      map[string]string // request field -> upload purpose
	PII                        bool
	Request                    any // zero value of the request DTO (nil when read-only)
	Response                   any // zero value of the response DTO
}

// Doc implements Registrar.
func (r Resource[Row, Req, Resp]) Doc() ResourceDoc {
	var req Req
	var resp Resp
	files := map[string]string{}
	for _, f := range r.Files {
		files[f.Field] = f.Purpose
	}
	return ResourceDoc{Table: r.Table, Label: r.Label, Module: r.Module, Path: r.Path, Import: r.Import,
		Sorts: r.Sorts, DefaultSort: r.DefaultSort, Filters: r.Filters, Nested: r.Nested, Files: files,
		PII: r.Mask != nil, Request: req, Response: resp}
}

// Doc implements Registrar.
func (r ReadResource[Row, Resp]) Doc() ResourceDoc {
	var resp Resp
	return ResourceDoc{Table: r.Table, Label: r.Label, Module: r.Module, Path: r.Path, ReadOnly: true,
		Sorts: r.Sorts, DefaultSort: r.DefaultSort, Filters: r.Filters, Nested: r.Nested, PII: r.Mask != nil,
		Response: resp}
}
