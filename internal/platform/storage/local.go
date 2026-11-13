package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

// LocalImageStorage writes files below a root directory using os.Root, which
// refuses any path that escapes the root (.., absolute paths, symlinks out).
type LocalImageStorage struct {
	root *os.Root
}

// NewLocalImageStorage opens (and creates if needed) the image root directory.
func NewLocalImageStorage(dir string) (*LocalImageStorage, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create image root: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open image root: %w", err)
	}
	return &LocalImageStorage{root: root}, nil
}

func (s *LocalImageStorage) Close() error { return s.root.Close() }

func validKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || path.Clean(key) != key || strings.Contains(key, "..") {
		return fmt.Errorf("invalid storage key %q", key)
	}
	return nil
}

// Put writes atomically: data goes to a temp file that is renamed into place.
func (s *LocalImageStorage) Put(_ context.Context, key string, r io.Reader) error {
	if err := validKey(key); err != nil {
		return err
	}
	if dir := path.Dir(key); dir != "." {
		if err := s.root.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
	}
	tmp := key + ".tmp"
	f, err := s.root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		_ = s.root.Remove(tmp)
		return fmt.Errorf("write: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = s.root.Remove(tmp)
		return fmt.Errorf("sync: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = s.root.Remove(tmp)
		return fmt.Errorf("close: %w", err)
	}
	if err := s.root.Rename(tmp, key); err != nil {
		_ = s.root.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

func (s *LocalImageStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	if err := validKey(key); err != nil {
		return nil, err
	}
	return s.root.Open(key)
}

func (s *LocalImageStorage) Delete(_ context.Context, key string) error {
	if err := validKey(key); err != nil {
		return err
	}
	if err := s.root.Remove(key); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Ping verifies the root is writable.
func (s *LocalImageStorage) Ping(_ context.Context) error {
	f, err := s.root.OpenFile(".ready", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	return f.Close()
}
