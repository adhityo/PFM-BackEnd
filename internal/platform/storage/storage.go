// Package storage stores images on the local filesystem and documents in S3 (PRD §8).
package storage

import (
	"context"
	"io"
	"time"
)

// ImageStorage keeps images on a local volume.
type ImageStorage interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// DocumentStorage keeps documents in a private object store.
type DocumentStorage interface {
	Bucket() string
	Put(ctx context.Context, key string, r io.Reader, size int64, contentType, sha256Hex string) error
	PresignGet(ctx context.Context, key, downloadName string, ttl time.Duration) (string, error)
	Delete(ctx context.Context, key string) error
	Ping(ctx context.Context) error
}
