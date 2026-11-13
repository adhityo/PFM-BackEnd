package main

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
)

// Column is a column of a migrated table.
type Column struct {
	Name      string
	DataType  string // information_schema data_type
	UDT       string // udt_name, e.g. _cidr for CIDR[]
	MaxLen    int
	Precision int
	Scale     int
	Nullable  bool
}

// Table is the introspected shape of a table.
type Table struct {
	Name       string
	Columns    []Column
	FKs        map[string]string // column -> referenced table
	SoftDelete bool
}

func (t Table) Column(name string) (Column, bool) {
	for _, c := range t.Columns {
		if c.Name == name {
			return c, true
		}
	}
	return Column{}, false
}

// Schema is every table of the public schema (except goose's).
type Schema map[string]*Table

func introspect(ctx context.Context, dsn string) (Schema, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	s := Schema{}
	rows, err := conn.Query(ctx, `
		SELECT table_name, column_name, data_type, udt_name,
		       COALESCE(character_maximum_length, 0), COALESCE(numeric_precision, 0), COALESCE(numeric_scale, 0),
		       is_nullable = 'YES'
		FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name <> 'goose_db_version'
		ORDER BY table_name, ordinal_position`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tbl string
		var c Column
		if err := rows.Scan(&tbl, &c.Name, &c.DataType, &c.UDT, &c.MaxLen, &c.Precision, &c.Scale, &c.Nullable); err != nil {
			return nil, err
		}
		t, ok := s[tbl]
		if !ok {
			t = &Table{Name: tbl, FKs: map[string]string{}}
			s[tbl] = t
		}
		t.Columns = append(t.Columns, c)
		if c.Name == "deleted_at" {
			t.SoftDelete = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	fkRows, err := conn.Query(ctx, `
		SELECT cl.relname, a.attname, rf.relname
		FROM pg_constraint c
		JOIN pg_class cl ON cl.oid = c.conrelid
		JOIN pg_class rf ON rf.oid = c.confrelid
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
		WHERE c.contype = 'f' AND cl.relnamespace = 'public'::regnamespace`)
	if err != nil {
		return nil, err
	}
	for fkRows.Next() {
		var tbl, col, ref string
		if err := fkRows.Scan(&tbl, &col, &ref); err != nil {
			return nil, err
		}
		if t, ok := s[tbl]; ok {
			t.FKs[col] = ref
		}
	}
	return s, fkRows.Err()
}

func (s Schema) names() []string {
	out := make([]string, 0, len(s))
	for n := range s {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
