package port

import "time"

type MetricsPort interface {
	RecordDuration(name string, d time.Duration, tags map[string]string)
	IncrCounter(name string, tags map[string]string)
	SetGauge(name string, v float64, tags map[string]string)
}
