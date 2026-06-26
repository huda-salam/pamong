package db

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/huda-salam/pamong/core"
	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/core/domain"
	"github.com/huda-salam/pamong/port"
)

// fakeResolver memetakan tenant_id ke lokasi DB tanpa menyentuh Postgres.
type fakeResolver struct {
	infos map[string]*port.TenantInfo
}

func (f *fakeResolver) Resolve(_ context.Context, tenantID string) (*port.TenantInfo, error) {
	info, ok := f.infos[tenantID]
	if !ok {
		return nil, core.ErrNotFound("Tenant", tenantID)
	}
	return info, nil
}

// newTestManager membuat manager dengan opener yang mencatat pemanggilan dan tidak
// benar-benar membuka koneksi (mengembalikan *Pool kosong unik tiap call).
func newTestManager(resolver port.TenantResolver) (*TenantConnManager, *[]connParams) {
	var calls []connParams
	m := NewTenantConnManager(resolver, config.DBConfig{
		Host: "shared-host", Port: 5432, User: "app", Password: "pw", PoolMax: 5,
	}, config.CentralDBConfig{Host: "central-host", Port: 5432, Name: "gov_central", User: "app"})
	m.open = func(_ context.Context, p connParams) (*Pool, error) {
		calls = append(calls, p)
		return &Pool{}, nil
	}
	return m, &calls
}

func TestTenantConnManager_TenantPoolCachedPerHostDB(t *testing.T) {
	resolver := &fakeResolver{infos: map[string]*port.TenantInfo{
		"pemkot-a": {TenantID: "pemkot-a", DBHost: "host1", DBName: "gov_a", IsActive: true},
		"pemkot-b": {TenantID: "pemkot-b", DBHost: "host1", DBName: "gov_b", IsActive: true},
	}}
	m, calls := newTestManager(resolver)
	ctx := context.Background()

	pA1, err := m.Tenant(ctx, "pemkot-a")
	if err != nil {
		t.Fatalf("tenant a: %v", err)
	}
	pA2, err := m.Tenant(ctx, "pemkot-a")
	if err != nil {
		t.Fatalf("tenant a lagi: %v", err)
	}
	if pA1 != pA2 {
		t.Fatal("pool tenant yang sama harus di-cache (pointer identik)")
	}

	pB, err := m.Tenant(ctx, "pemkot-b")
	if err != nil {
		t.Fatalf("tenant b: %v", err)
	}
	if pB == pA1 {
		t.Fatal("DB berbeda pada host sama harus pool berbeda")
	}

	// Hanya dua open: (host1, gov_a) dan (host1, gov_b).
	if len(*calls) != 2 {
		t.Fatalf("open dipanggil %d kali, mau 2", len(*calls))
	}
	// Kredensial+pool BERSAMA dari config ikut ke tiap pool tenant (ADR-004).
	if (*calls)[0].User != "app" || (*calls)[0].Password != "pw" || (*calls)[0].PoolMax != 5 {
		t.Fatalf("pool tenant tidak memakai kredensial bersama: %+v", (*calls)[0])
	}
	if (*calls)[0].Host != "host1" || (*calls)[0].Name != "gov_a" {
		t.Fatalf("lokasi tenant a salah: %+v", (*calls)[0])
	}
}

func TestTenantConnManager_TenantFallbackHost(t *testing.T) {
	// DBHost kosong di registry → pakai host default Tier 1 dari config (shared).
	resolver := &fakeResolver{infos: map[string]*port.TenantInfo{
		"pemkot-c": {TenantID: "pemkot-c", DBHost: "", DBName: "gov_c", IsActive: true},
	}}
	m, calls := newTestManager(resolver)

	if _, err := m.Tenant(context.Background(), "pemkot-c"); err != nil {
		t.Fatalf("tenant c: %v", err)
	}
	if (*calls)[0].Host != "shared-host" {
		t.Fatalf("host fallback salah: %q (mau shared-host)", (*calls)[0].Host)
	}
}

func TestTenantConnManager_CentralPoolSingletonAndRouting(t *testing.T) {
	resolver := &fakeResolver{infos: map[string]*port.TenantInfo{
		"pemkot-a": {TenantID: "pemkot-a", DBHost: "host1", DBName: "gov_a", IsActive: true},
	}}
	m, calls := newTestManager(resolver)
	ctx := context.Background()

	centralEntity := domain.EntityDef{Name: "KodeWilayah", Residency: domain.ResidencyCentral}
	tenantEntity := domain.EntityDef{Name: "SuratMasuk"} // default ResidencyTenant

	// Entity central → central pool (host central, abaikan tenant id).
	c1, err := m.For(ctx, "pemkot-a", centralEntity)
	if err != nil {
		t.Fatalf("central: %v", err)
	}
	c2, err := m.For(ctx, "pemkot-lain", centralEntity)
	if err != nil {
		t.Fatalf("central lagi: %v", err)
	}
	if c1 != c2 {
		t.Fatal("central pool harus singleton lintas tenant")
	}
	if (*calls)[0].Host != "central-host" || (*calls)[0].Name != "gov_central" {
		t.Fatalf("central pool tidak memakai config central: %+v", (*calls)[0])
	}

	// Entity tenant → pool tenant (bukan central).
	tp, err := m.For(ctx, "pemkot-a", tenantEntity)
	if err != nil {
		t.Fatalf("tenant routing: %v", err)
	}
	if tp == c1 {
		t.Fatal("entity tenant tidak boleh dirutekan ke central pool")
	}
	// 2 open: 1 central + 1 tenant.
	if len(*calls) != 2 {
		t.Fatalf("open dipanggil %d kali, mau 2", len(*calls))
	}
}

func TestTenantConnManager_ResolveErrorPropagates(t *testing.T) {
	m, calls := newTestManager(&fakeResolver{infos: map[string]*port.TenantInfo{}})
	if _, err := m.Tenant(context.Background(), "tak-ada"); err == nil {
		t.Fatal("tenant tak dikenal harus error")
	}
	if len(*calls) != 0 {
		t.Fatal("open tidak boleh dipanggil saat resolve gagal")
	}
}

// TestTenantConnManager_SameKeyOpenedOnce: banyak goroutine concurrent untuk tenant yang
// sama hanya menghasilkan SATU pembukaan pool (key sama diserialisasi di lock per-entry).
func TestTenantConnManager_SameKeyOpenedOnce(t *testing.T) {
	resolver := &fakeResolver{infos: map[string]*port.TenantInfo{
		"t": {TenantID: "t", DBHost: "h", DBName: "d", IsActive: true},
	}}
	m := NewTenantConnManager(resolver, config.DBConfig{Port: 5432, User: "app"}, config.CentralDBConfig{})
	var mu sync.Mutex
	count := 0
	m.open = func(_ context.Context, _ connParams) (*Pool, error) {
		mu.Lock()
		count++
		mu.Unlock()
		return &Pool{}, nil
	}

	const g = 24
	var wg sync.WaitGroup
	pools := make([]*Pool, g)
	for i := 0; i < g; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := m.Tenant(context.Background(), "t")
			if err != nil {
				t.Errorf("tenant: %v", err)
				return
			}
			pools[i] = p
		}(i)
	}
	wg.Wait()

	if count != 1 {
		t.Fatalf("open dipanggil %d kali untuk key sama, mau 1", count)
	}
	for i := 1; i < g; i++ {
		if pools[i] != pools[0] || pools[i] == nil {
			t.Fatalf("semua goroutine harus dapat pool ter-cache yang sama")
		}
	}
}

// TestTenantConnManager_DifferentKeysOpenConcurrently membuktikan tidak ada head-of-line
// blocking antar tenant: open untuk key berbeda berjalan BERSAMAAN. Bila pembukaan masih
// di bawah satu lock global, hanya satu open jalan pada satu waktu → barrier tak pernah
// lengkap → timeout → test gagal.
func TestTenantConnManager_DifferentKeysOpenConcurrently(t *testing.T) {
	const n = 6
	infos := map[string]*port.TenantInfo{}
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("t%d", i)
		infos[id] = &port.TenantInfo{TenantID: id, DBHost: fmt.Sprintf("h%d", i), DBName: "d", IsActive: true}
	}
	m := NewTenantConnManager(&fakeResolver{infos: infos}, config.DBConfig{Port: 5432, User: "app"}, config.CentralDBConfig{})

	var arrived sync.WaitGroup
	arrived.Add(n)
	release := make(chan struct{})
	m.open = func(_ context.Context, _ connParams) (*Pool, error) {
		arrived.Done() // tandai goroutine ini sudah masuk open
		<-release      // tahan sampai semua n tiba bersamaan
		return &Pool{}, nil
	}

	for i := 0; i < n; i++ {
		id := fmt.Sprintf("t%d", i)
		go func() { _, _ = m.Tenant(context.Background(), id) }()
	}

	allArrived := make(chan struct{})
	go func() { arrived.Wait(); close(allArrived) }()

	select {
	case <-allArrived:
		close(release) // semua open berjalan bersamaan — bagus
	case <-time.After(2 * time.Second):
		t.Fatal("open antar key ter-serialisasi (head-of-line blocking)")
	}
}
