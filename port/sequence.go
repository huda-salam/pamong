package port

import "context"

type SequenceGenerator interface {
	Next(ctx context.Context, tenantID string, pattern string, tahun int) (string, error)
}
