// Package db holds the embedded goose migrations (one set per database) and
// the sqlc queries.
package db

import (
	"embed"
	"fmt"
	"io/fs"
	"slices"
)

// The four databases (PRD §7.1). Each has its own connection, credentials and
// goose history, so a compromise of one does not expose the others.
const (
	Core     = "core"     // business data
	Security = "security" // users, roles, permissions, refresh tokens
	Log      = "log"      // audit trail (append-only)
	Document = "document" // file metadata and the document module
	// All is the union of every set: one database used only by the code
	// generator (cmd/scaffold) and sqlc to see the whole schema.
	All = "all"
)

// Databases lists the runtime databases in migration order.
var Databases = []string{Core, Security, Log, Document}

//go:embed migrations
var migrations embed.FS

// layers are the migration folders of each set. Versions are unique across
// folders (global numbering of PRD Appendix A), so any combination is a valid
// goose sequence.
var layers = map[string][]string{
	Core:     {"shared", "outbox", "core"},
	Security: {"shared", "outbox", "security"},
	Log:      {"shared", "log"},
	Document: {"shared", "outbox", "document"},
	All:      {"shared", "outbox", "core", "security", "log", "document"},
}

// Migrations returns the migration files of one set as a flat directory.
func Migrations(name string) (fs.FS, error) {
	dirs, ok := layers[name]
	if !ok {
		return nil, fmt.Errorf("database tidak dikenal: %q", name)
	}
	var out layered
	for _, d := range dirs {
		sub, err := fs.Sub(migrations, "migrations/"+d)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, nil
}

// layered merges directories into one flat fs.FS; file names must be unique.
type layered []fs.FS

func (l layered) Open(name string) (fs.File, error) {
	for _, f := range l {
		file, err := f.Open(name)
		if err == nil {
			return file, nil
		}
	}
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (l layered) ReadDir(name string) ([]fs.DirEntry, error) {
	var out []fs.DirEntry
	for _, f := range l {
		entries, err := fs.ReadDir(f, name)
		if err != nil {
			return nil, err
		}
		out = append(out, entries...)
	}
	slices.SortFunc(out, func(a, b fs.DirEntry) int {
		switch {
		case a.Name() < b.Name():
			return -1
		case a.Name() > b.Name():
			return 1
		}
		return 0
	})
	for i := 1; i < len(out); i++ {
		if out[i].Name() == out[i-1].Name() {
			return nil, fmt.Errorf("migrasi ganda: %s", out[i].Name())
		}
	}
	return out, nil
}
