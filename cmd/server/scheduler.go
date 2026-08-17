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
	runner *scheduler.Runner
	// deadlines dibungkus tolerantDeadlines — tipenya port, bukan adapter konkret, supaya
	// pembungkus itu tak bisa dilewati tanpa mengubah tipe di sini.
	deadlines coreWf.DeadlineScheduler
}

// wireScheduler merakit store+locker sentral, mendaftarkan handler job, dan mengembalikan runner
// yang SIAP dijalankan (belum berjalan — run() yang memulainya agar shutdown-nya satu tempat).
//
// centralPool WAJIB pool DB sentral. Ini tak bisa dipaksakan oleh tipe (semua pool bertipe sama),
// jadi ia dijaga oleh satu-satunya pemanggil di run() plus jalur migrasi terpisah
// (`pamongctl migrate --central`); memberi pool tenant di sini akan "berhasil" dan menghasilkan
// scheduler yang hanya melihat jadwal satu tenant.
func wireScheduler(ctx context.Context, centralPool *db.Pool, pools tenantPoolProvider,
	notifs *notificationFactory, interval, lockTTL time.Duration,
	metrics port.MetricsPort, logger port.Logger) (*schedulerStack, error) {

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
		runner: runner,
		deadlines: &tolerantDeadlines{
			inner:   infrawf.NewSchedulerDeadlines(runner, store),
			metrics: metrics,
			logger:  logger,
		},
	}, nil
}

// tolerantDeadlines mencatat kegagalan penjadwalan/pembatalan deadline SLA lalu MENGEMBALIKAN nil,
// dengan alasan yang persis sama dengan tolerantTransitionNotifier — dan disatukan justru karena
// keduanya mudah diperlakukan berbeda tanpa sengaja.
//
// Engine memanggil cancelSLA/scheduleSLA SESUDAH transisi otoritatif, dan handler menyimpan
// instance sebelum mengembalikan error. Jadi error dari sini akan merespons 5xx atas transisi yang
// BERHASIL, dan respons wajar klien — retry — berbahaya karena aksi bisnisnya sudah dijalankan.
//
// Penting untuk jujur soal apa yang HILANG, karena di sini taruhannya lebih besar daripada
// notifikasi: deadline yang gagal dijadwalkan berarti SLA state itu tak akan pernah mengeskalasi.
// Tapi 5xx tidak memulihkannya juga — deadline sama-sama hilang di kedua pilihan; yang membedakan
// hanya apakah kliennya ikut dibohongi. Metrik di bawah adalah cara melihat kehilangan itu, dan
// pemulihan yang sesungguhnya (rekonsiliasi deadline dari state instance) dicatat di backlog.
//
// CancelDeadline lebih ringan: kegagalannya sudah punya backstop di fire-time — EscalationCoordinator
// meng-no-op-kan deadline yang instance-nya sudah pindah state.
type tolerantDeadlines struct {
	inner   coreWf.DeadlineScheduler
	metrics port.MetricsPort
	logger  port.Logger
}

var _ coreWf.DeadlineScheduler = (*tolerantDeadlines)(nil)

func (d *tolerantDeadlines) ScheduleDeadline(ctx context.Context, dl coreWf.Deadline) error {
	if err := d.inner.ScheduleDeadline(ctx, dl); err != nil {
		d.laporkan(ctx, "schedule", dl.Escalation.TenantID, dl.Key, err,
			"deadline SLA GAGAL dijadwalkan; transisi tetap tersimpan tapi state ini tak akan mengeskalasi")
	}
	return nil
}

func (d *tolerantDeadlines) CancelDeadline(ctx context.Context, key string) error {
	if err := d.inner.CancelDeadline(ctx, key); err != nil {
		d.laporkan(ctx, "cancel", "", key, err,
			"pembatalan deadline SLA gagal; backstop guard race di fire-time yang menutupnya")
	}
	return nil
}

func (d *tolerantDeadlines) laporkan(ctx context.Context, op, tenantID, key string, err error, pesan string) {
	if d.metrics != nil {
		d.metrics.IncrCounter("workflow_sla_deadline_failed_total", map[string]string{
			"op": op, "tenant": tenantID,
		})
	}
	if d.logger != nil {
		d.logger.Error(ctx, pesan,
			port.F("op", op), port.F("tenant", tenantID),
			port.F("deadline_key", key), port.F("err", err.Error()))
	}
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
		// forPool, bukan ForTenant: pool tenant ini SUDAH di-resolve sebaris di atas, dan
		// TenantConnManager.Tenant membaca id.tenant_registry pada setiap panggilan.
		notifier, err := notifs.forPool(ctx, tenantID, pool)
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
