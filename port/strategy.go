package port

import "context"

type StrategyRegistry interface {
	Register(key string, impl any) error
	Resolve(ctx context.Context, tenantID string, point string) (any, error)
	AvailableOptions(ctx context.Context, tenantID string, point string) ([]string, error)
}
