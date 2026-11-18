package config

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

var reEnvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// loadDotEnv sets variables from a KEY=VALUE file for local development, so
// `go run ./cmd/api` and air behave like `make run`. Variables already set in
// the environment always win, and a missing file is not an error.
//
// Supported syntax: blank lines, `# comment` lines, an optional `export `
// prefix, values in "double" or 'single' quotes, and ` # comment` after an
// unquoted value.
func loadDotEnv(path string) error {
	f, err := os.Open(path) //nolint:gosec // developer-chosen local file (.env or ENV_FILE)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("baca %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || !reEnvKey.MatchString(key) {
			return fmt.Errorf("%s baris %d: format harus KEY=VALUE", path, n)
		}
		if _, set := os.LookupEnv(key); set {
			continue
		}
		if err := os.Setenv(key, unquote(strings.TrimSpace(value))); err != nil {
			return err
		}
	}
	return sc.Err()
}

func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}
