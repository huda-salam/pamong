package port

import (
	"context"
	"github.com/google/uuid"
)

type BaseRepository[T any] interface {
	FindByID(ctx context.Context, id uuid.UUID) (*T, error)
	Save(ctx context.Context, entity *T) error
	Update(ctx context.Context, entity *T) error
	SoftDelete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context, filter ListFilter) (*ListResult[T], error)
}

type ListFilter struct {
	Page     int
	PageSize int
	Sort     string
	Order    string
	Search   string
	Filters  map[string]any
}

type ListResult[T any] struct {
	Items      []T
	Total      int64
	Page       int
	PageSize   int
	TotalPages int
}
