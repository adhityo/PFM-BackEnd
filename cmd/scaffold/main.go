// Command scaffold generates CRUD plumbing from the migrated schema (dev tool).
//
//	scaffold schema            -> internal/db/schema/schema_gen.go
//	scaffold module <name>     -> per table of the module: internal/db/queries/<table>.sql,
//	                              internal/dto/<table>_gen.go, internal/repository/<table>_gen.go,
//	                              internal/service/<table>_gen.go; plus internal/service/resources_<name>_gen.go
//
// It introspects SCAFFOLD_DATABASE_URL (a database migrated with `migrate up`).
// Generated files are marked "DO NOT EDIT": change the spec or add a hook in
// a hand-written file instead, then regenerate and run `sqlc generate`.
package main

import (
	"context"
	"errors"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "scaffold:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("pakai: scaffold schema | scaffold module <nama>")
	}
	dsn := os.Getenv("SCAFFOLD_DATABASE_URL")
	if dsn == "" {
		return errors.New("SCAFFOLD_DATABASE_URL wajib diisi (database yang sudah di-migrate)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := introspect(ctx, dsn)
	if err != nil {
		return err
	}

	switch args[0] {
	case "schema":
		return writeGo("internal/db/schema/schema_gen.go", genSchema(s))
	case "module":
		if len(args) < 2 {
			return errors.New("nama module wajib diisi")
		}
		specs, ok := moduleSpecs[args[1]]
		if !ok {
			return fmt.Errorf("module %q tidak punya spec", args[1])
		}
		for _, sp := range specs {
			t, ok := s[sp.Table]
			if !ok {
				return fmt.Errorf("tabel %s tidak ada di database", sp.Table)
			}
			sp.Module = args[1]
			sp.applyDefaults(t)
			for _, c := range append(append(append([]string{}, sp.Required...), sp.Search...), sp.ReadOnly...) {
				mustColumn(t, c, sp.Table)
			}
			if err := os.WriteFile(filepath.Join("internal/db/queries", sp.Table+".sql"), []byte(genSQL(sp, t)), 0o644); err != nil { //nolint:gosec // source file
				return err
			}
			p, err := newPlan(sp, t)
			if err != nil {
				return err
			}
			svc, err := genService(p)
			if err != nil {
				return err
			}
			for path, src := range map[string]string{
				filepath.Join("internal/dto", sp.Table+"_gen.go"):        genDTO(p),
				filepath.Join("internal/repository", sp.Table+"_gen.go"): genRepository(p),
				filepath.Join("internal/service", sp.Table+"_gen.go"):    svc,
			} {
				if err := writeGo(path, src); err != nil {
					return err
				}
			}
			fmt.Println("generated", sp.Table)
		}
		return writeGo(filepath.Join("internal/service", "resources_"+args[1]+"_gen.go"), genRegistry(args[1], specs))
	default:
		return fmt.Errorf("perintah tidak dikenal: %s", args[0])
	}
}

// writeGo formats and writes generated source. Paths are built from table and
// module names that exist in moduleSpecs, never from free-form input.
func writeGo(path, src string) error {
	out, err := format.Source([]byte(src))
	if err != nil {
		_ = os.WriteFile(path+".broken", []byte(src), 0o600) //nolint:gosec // path from known spec names
		return fmt.Errorf("format %s: %w (lihat %s.broken)", path, err, path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { //nolint:gosec // path from known spec names
		return err
	}
	return os.WriteFile(path, out, 0o644) //nolint:gosec // source file
}
