package port

import (
	"context"
	"time"
)

type FiscalPeriodStatus string

const (
	FiscalOpen       FiscalPeriodStatus = "open"
	FiscalSoftClosed FiscalPeriodStatus = "soft_closed"
	FiscalHardClosed FiscalPeriodStatus = "hard_closed"
)

type FiscalChecker interface {
	CheckPeriod(ctx context.Context, tenantID string, date time.Time) (FiscalPeriodStatus, error)
}
