package eventbus_test

import (
	"testing"
	"time"

	"github.com/huda-salam/pamong/infra/eventbus"
)

// ===== RetryPolicy.NextRetry =====

func TestRetryPolicy_PertamaKaliGagal_BackoffBase(t *testing.T) {
	p := eventbus.RetryPolicy{MaxAttempts: 5, BackoffBase: 10 * time.Second, BackoffMax: time.Hour}
	before := time.Now()
	nextRetry, isDLQ := p.NextRetry(1)
	after := time.Now()

	if isDLQ {
		t.Fatal("attempts=1 dari MaxAttempts=5 harus bukan DLQ")
	}
	// nextRetry harus dalam rentang [before+BackoffBase, after+BackoffBase+epsilon]
	low := before.Add(10 * time.Second)
	high := after.Add(10*time.Second + time.Millisecond)
	if nextRetry.Before(low) || nextRetry.After(high) {
		t.Errorf("backoff pertama harus ~10s, dapat: %v (rentang %v–%v)", nextRetry, low, high)
	}
}

func TestRetryPolicy_BackoffEksponensial(t *testing.T) {
	p := eventbus.RetryPolicy{MaxAttempts: 10, BackoffBase: 1 * time.Second, BackoffMax: time.Hour}

	// attempt=1 → 1s, attempt=2 → 2s, attempt=3 → 4s, attempt=4 → 8s
	expected := []time.Duration{1, 2, 4, 8}
	for i, want := range expected {
		attempts := i + 1
		before := time.Now()
		nextRetry, isDLQ := p.NextRetry(attempts)
		after := time.Now()

		if isDLQ {
			t.Fatalf("attempts=%d harus bukan DLQ (MaxAttempts=10)", attempts)
		}
		low := before.Add(want * time.Second)
		high := after.Add(want*time.Second + time.Millisecond)
		if nextRetry.Before(low) || nextRetry.After(high) {
			t.Errorf("attempts=%d: mau ~%ds, dapat offset=%v", attempts, want, nextRetry.Sub(before))
		}
	}
}

func TestRetryPolicy_BackoffCap(t *testing.T) {
	p := eventbus.RetryPolicy{MaxAttempts: 20, BackoffBase: time.Minute, BackoffMax: 5 * time.Minute}
	before := time.Now()
	// attempts=10 → base * 2^9 = 512 menit >> cap 5 menit
	nextRetry, isDLQ := p.NextRetry(10)
	if isDLQ {
		t.Fatal("attempts=10 harus bukan DLQ (MaxAttempts=20)")
	}
	// Harus di-cap ke BackoffMax = 5 menit
	low := before.Add(5 * time.Minute)
	high := before.Add(5*time.Minute + time.Millisecond)
	if nextRetry.Before(low) || nextRetry.After(high) {
		t.Errorf("backoff harus di-cap 5 menit, dapat offset=%v dari before", nextRetry.Sub(before))
	}
}

func TestRetryPolicy_DLQ_PadaMaxAttempts(t *testing.T) {
	p := eventbus.RetryPolicy{MaxAttempts: 5, BackoffBase: time.Second, BackoffMax: time.Hour}

	_, isDLQ := p.NextRetry(5) // attempts == MaxAttempts → DLQ
	if !isDLQ {
		t.Error("attempts=5 == MaxAttempts=5 harus DLQ")
	}
}

func TestRetryPolicy_DLQ_MelebihiMaxAttempts(t *testing.T) {
	p := eventbus.RetryPolicy{MaxAttempts: 3, BackoffBase: time.Second, BackoffMax: time.Hour}

	_, isDLQ := p.NextRetry(10) // attempts jauh melebihi MaxAttempts
	if !isDLQ {
		t.Error("attempts=10 >> MaxAttempts=3 harus DLQ")
	}
}

func TestRetryPolicy_BelumDLQ_SatuSebelumMax(t *testing.T) {
	p := eventbus.RetryPolicy{MaxAttempts: 5, BackoffBase: time.Second, BackoffMax: time.Hour}

	_, isDLQ := p.NextRetry(4) // attempts = MaxAttempts - 1 → belum DLQ
	if isDLQ {
		t.Error("attempts=4 < MaxAttempts=5 belum DLQ")
	}
}

func TestDefaultRetryPolicy_DefaultSensible(t *testing.T) {
	p := eventbus.DefaultRetryPolicy()
	if p.MaxAttempts <= 0 {
		t.Errorf("MaxAttempts harus > 0, dapat %d", p.MaxAttempts)
	}
	if p.BackoffBase <= 0 {
		t.Errorf("BackoffBase harus > 0, dapat %v", p.BackoffBase)
	}
	if p.BackoffMax < p.BackoffBase {
		t.Errorf("BackoffMax %v harus >= BackoffBase %v", p.BackoffMax, p.BackoffBase)
	}
}
