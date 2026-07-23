package notification_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/huda-salam/pamong/core/notification"
)

func kadis() notification.RoleTarget {
	return notification.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas"}
}

func recip() notification.Recipient {
	return notification.Recipient{PersonID: uuid.New()}
}

// --- Router: kebijakan fallback PLT ---

func TestRouter_HolderDipilih(t *testing.T) {
	dir := notification.NewMemoryDirectory()
	holder := recip()
	dir.SetHolders(kadis(), holder)
	dir.SetActing(kadis(), recip()) // PLT ada, TAPI tak dipakai saat pejabat definitif ada

	got, err := notification.NewRouter(dir).Resolve(context.Background(), kadis())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 || got[0].PersonID != holder.PersonID {
		t.Fatalf("mau pemegang definitif, dapat %+v", got)
	}
}

func TestRouter_JabatanKosongJatuhKePLT(t *testing.T) {
	// DoD PR-3.6.2: notif ke "Kadis" jatuh ke PLT bila jabatan kosong.
	dir := notification.NewMemoryDirectory()
	plt := recip()
	dir.SetActing(kadis(), plt) // pemegang kosong; hanya PLT

	got, err := notification.NewRouter(dir).Resolve(context.Background(), kadis())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(got) != 1 || got[0].PersonID != plt.PersonID {
		t.Fatalf("mau PLT saat jabatan kosong, dapat %+v", got)
	}
}

func TestRouter_TanpaPemegangDanPLT(t *testing.T) {
	dir := notification.NewMemoryDirectory()
	if _, err := notification.NewRouter(dir).Resolve(context.Background(), kadis()); err == nil {
		t.Fatal("mau ErrNoRecipient saat peran tak bertuan")
	}
}

func TestRouter_UnitKerjaMembedakanTarget(t *testing.T) {
	dukcapil := uuid.New()
	dinkes := uuid.New()
	tDukcapil := notification.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas", UnitKerjaID: &dukcapil}
	tDinkes := notification.RoleTarget{TenantID: "pemkot", Role: "kepala_dinas", UnitKerjaID: &dinkes}

	dir := notification.NewMemoryDirectory()
	kadisDukcapil := recip()
	dir.SetHolders(tDukcapil, kadisDukcapil)
	// Dinkes sengaja tak diisi.

	got, err := notification.NewRouter(dir).Resolve(context.Background(), tDukcapil)
	if err != nil {
		t.Fatalf("resolve dukcapil: %v", err)
	}
	if len(got) != 1 || got[0].PersonID != kadisDukcapil.PersonID {
		t.Fatalf("target unit berbeda tercampur: %+v", got)
	}
	if _, err := notification.NewRouter(dir).Resolve(context.Background(), tDinkes); err == nil {
		t.Fatal("target unit lain (dinkes) harusnya tak bertuan")
	}
}

// --- RoleNotifier: routing + hub end-to-end (in-app) ---

func newInAppNotifier(t *testing.T, dir notification.RecipientDirectory) (*notification.RoleNotifier, *notification.MemoryInAppInbox) {
	t.Helper()
	inbox := notification.NewMemoryInAppInbox()
	reg := notification.NewChannelRegistry()
	if err := reg.Register(notification.NewInAppChannel(inbox)); err != nil {
		t.Fatalf("register channel: %v", err)
	}
	eng := newEngine(t, notification.Template{Key: "wf.eskalasi", Locale: "id", Subject: "Eskalasi", Body: "Tindak lanjuti {{.perihal}}"})
	hub := notification.NewHub(reg, eng, notification.NewMemoryDeliveryRecorder())
	return notification.NewRoleNotifier(notification.NewRouter(dir), hub), inbox
}

func TestRoleNotifier_KirimKePemegang(t *testing.T) {
	dir := notification.NewMemoryDirectory()
	holder := recip()
	dir.SetHolders(kadis(), holder)
	notifier, inbox := newInAppNotifier(t, dir)

	count, err := notifier.NotifyRole(context.Background(), kadis(), "wf.eskalasi",
		map[string]any{"perihal": "SPM-001"}, notification.ChannelInApp)
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, mau 1 penerima", count)
	}
	items, _ := inbox.List(context.Background(), "pemkot", holder.PersonID.String(), 0)
	if len(items) != 1 || items[0].Body != "Tindak lanjuti SPM-001" {
		t.Fatalf("inbox pemegang = %+v", items)
	}
}

func TestRoleNotifier_JabatanKosongKirimKePLT(t *testing.T) {
	dir := notification.NewMemoryDirectory()
	plt := recip()
	dir.SetActing(kadis(), plt)
	notifier, inbox := newInAppNotifier(t, dir)

	count, err := notifier.NotifyRole(context.Background(), kadis(), "wf.eskalasi",
		map[string]any{"perihal": "SPM-001"}, notification.ChannelInApp)
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d", count)
	}
	if items, _ := inbox.List(context.Background(), "pemkot", plt.PersonID.String(), 0); len(items) != 1 {
		t.Fatalf("PLT tak menerima notif: %+v", items)
	}
}

func TestRoleNotifier_MultiPenerima(t *testing.T) {
	dir := notification.NewMemoryDirectory()
	a, b := recip(), recip()
	dir.SetHolders(kadis(), a, b) // mis. dua pejabat memegang peran sama
	notifier, inbox := newInAppNotifier(t, dir)

	count, err := notifier.NotifyRole(context.Background(), kadis(), "wf.eskalasi",
		map[string]any{"perihal": "x"}, notification.ChannelInApp)
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, mau 2", count)
	}
	for _, r := range []notification.Recipient{a, b} {
		if items, _ := inbox.List(context.Background(), "pemkot", r.PersonID.String(), 0); len(items) != 1 {
			t.Fatalf("penerima %s tak dapat notif", r.PersonID)
		}
	}
}

func TestRoleNotifier_PeranTakBertuanGagalTanpaKirim(t *testing.T) {
	notifier, _ := newInAppNotifier(t, notification.NewMemoryDirectory())
	count, err := notifier.NotifyRole(context.Background(), kadis(), "wf.eskalasi", nil, notification.ChannelInApp)
	if err == nil {
		t.Fatal("mau error peran tak bertuan")
	}
	if count != 0 {
		t.Fatalf("count = %d, mau 0", count)
	}
}
