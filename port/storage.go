package port

import (
	"context"
	"io"
)

type StorageMeta struct {
	ContentType string
	Module      string
	EntityID    string
	TenantID    string
}

type StoragePort interface {
	Upload(ctx context.Context, key string, r io.Reader, meta StorageMeta) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]string, error)
}
