// scheduler.go merakit runtime SCHEDULER di composition root (PR-W4b, ADR-023): satu loop
// proses-lebar di atas DB SENTRAL, ditambah handler eskalasi SLA yang me-resolve tumpukan
// per-tenant dari tenant yang dibawa baris job.
//
// Kenapa SATU loop dan bukan satu per tenant — kebalikan dari workflow.go, dan itu disengaja:
// pembaca jadwal (Runner.RunDue) tidak berada di dalam tenant mana pun; ia bertanya "apa yang
// jatuh tempo, di mana saja?". Residensi mengikuti pembaca (ADR-023 Keputusan 1), jadi tabelnya
// sentral dan loop-nya tunggal. Yang tetap per-tenant adalah EKSEKUSI-nya: handler menerima
// tenant lewat ctx dan merakit store/notifier tenant itu saat berjalan.
package main

import (
	"context"
	"fmt"
	"time"

	coreNotif "github.com/huda-salam/pamong/core/notification"
	"github.com/huda-salam/pamong/core/scheduler"
	coreWf "github.com/huda-salam/pamong/core/workflow"
	"github.com/huda-salam/pamong/infra/db"
	infrasched "github.com/huda-salam/pamong/infra/scheduler"
	infrawf "github.com/huda-salam/pamong/infra/workflow"
	"github.com/huda-salam/pamong/port"
)

// EscalationTemplateKey adalah key template notifikasi yang dipakai eskalasi SLA. Default
// GLOBAL-nya ditanam framework saat skema notifikasi tenant disiapkan (seedFrameworkTemplates),
// jadi eskalasi bekerja pada instalasi baru tanpa tenant menyeed apa pun; tenant tetap bisa
// meng-override dengan baris ber-tenant sendiri.
//
// Bila template tetap tak ada, jejaknya ada di baris GAGAL `gov.job_runs` — BUKAN di
// gov.notification_deliveries. Hub.Send sengaja tak mencatat kegagalan pra-dispatch (template tak
// ditemukan / gagal render) sebagai delivery: itu diklasifikasikan sebagai bug konfigurasi, bukan
// kegagalan pengiriman. Perbedaan itu menentukan tabel mana yang dilihat ops saat menyelidiki.
const EscalationTemplateKey = "workflow.sla.escalation"

// schedulerStack adalah hasil perakitan scheduler yang dibutuhkan run(): runner untuk dijalankan
// & ditunggu saat shutdown, dan deadlines untuk dipasang ke setiap engine workflow per tenant.
type schedulerStack struct {
	runner    *scheduler.Runner
	deadlines *infrawf.SchedulerDeadlines
}

// wireScheduler merakit store+locker sentral, mendaftarkan handler job, dan mengembalikan runner
// yang SIAP dijalankan (belum berjalan — run() yang memulainya agar shutdown-nya satu tempat).
//
// centralPool WAJIB pool DB sentral. Ini tak bisa dipaksakan oleh tipe (semua pool bertipe sama),
// jadi ia dijaga oleh satu-satunya pemanggil di run() plus jalur migrasi terpisah
// (`pamongctl migrate --central`); memberi pool tenant di sini akan "berhasil" dan menghasilkan
// scheduler yang hanya melihat jadwal satu tenant.
func wireScheduler(ctx context.Context, centralPool *db.Pool, pools tenantPoolProvider,
	notifs *notificationFactory, interval, lockTTL time.Duration, logger port.Logger) (*schedulerStack, error) {

	store := infrasched.NewDBJobStore(centralPool)
	if err := store.EnsureSchema(ctx); err != nil {
		return nil, fmt.Errorf("schema scheduler (DB sentral, ADR-023): %w", err)
	}
	locker := infrasched.NewDBLocker(centralPool)

	registry := scheduler.NewRegistry()
	if err := registry.Register(infrawf.EscalationJobKey, escalationJob(pools, notifs)); err != nil {
		return nil, fmt.Errorf("daftarkan handler eskalasi SLA: %w", err)
	}

	// Locker dipasang TANPA SYARAT, bukan hanya "bila multi-instance": tak ada tempat di config
	// yang tahu berapa replika yang sedang berjalan, dan default yang aman untuk pertanyaan itu
	// adalah menganggap ada lebih dari satu. Pada single-instance biayanya satu upsert per job.
	runner := scheduler.NewRunner(registry, store, interval).WithLocker(locker, lockTTL)

	logger.Info(ctx, "scheduler terpasang (DB sentral, ADR-023)",
		port.F("interval", interval.String()),
		port.F("lock_ttl", lockTTL.String()),
		port.F("job", registry.Keys()))

	return &schedulerStack{
		runner:    runner,
		deadlines: infrawf.NewSchedulerDeadlines(runner, store),
	}, nil
}

// escalationJob adalah handler EscalationJobKey pada server hidup. Ia me-resolve tumpukan tenant
// SAAT BERJALAN, dari tenant yang disisipkan Runner ke ctx (ADR-023 Keputusan 5).
//
// Perakitan tak bisa dilakukan sekali saat boot: instance store dan notifier hidup di tenant DB,
// dan tenant mana yang dilayani baru diketahui saat baris job dibaca. Biayanya mendekati nol —
// keduanya struct tipis di atas pool yang sudah ter-cache.
func escalationJob(pools tenantPoolProvider, notifs *notificationFactory) scheduler.JobFunc {
	return func(ctx context.Context, payload []byte) error {
		tenantID := port.TenantFrom(ctx)
		if tenantID == "" {
			// Fail-closed. Tanpa tenant tak ada DB yang bisa dibaca, dan menebak (mis. dari
			// payload) akan menjadikan payload sebagai otoritas tenant — persis yang dicegah
			// pagar di infrawf.EscalationJob.
			return fmt.Errorf("job eskalasi tanpa tenant di context: baris job kehilangan tenant_id")
		}
		pool, err := pools.Tenant(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("pool tenant %q: %w", tenantID, err)
		}
		notifier, err := notifs.ForTenant(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("tumpukan notifikasi tenant %q: %w", tenantID, err)
		}
		// Guard race eskalasi (instance masih di state ber-SLA?) dibaca dari instance store
		// tenant itu — sumber kebenaran yang sama dengan yang dipakai jalur HTTP.
		coord := coreWf.NewEscalationCoordinator(
			infrawf.NewDBInstanceStore(pool),
			infrawf.NewNotifierEscalator(notifier, EscalationTemplateKey, coreNotif.ChannelInApp),
		)
		return infrawf.EscalationJob(coord)(ctx, payload)
	}
}
