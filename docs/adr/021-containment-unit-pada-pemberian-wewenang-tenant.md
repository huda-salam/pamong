# ADR-021: Containment unit kerja pada pemberian wewenang tenant

## Status
Accepted

**Amends ADR-019** (provenance role & containment wewenang) — memperluas prinsipnya dari lapis
SENTRAL (identity: siapa boleh memberi role sentral/kredensial kepada siapa) ke lapis TENANT
(siapa boleh memberi role tenant & delegasi, DI MANA). Keputusan ADR-019 tidak berubah; yang
ditambahkan adalah sumbu kedua — jangkauan data — beserta cara menanyakannya.

## Konteks
Lapis ABAC Pamong sudah lengkap sejak PR-2.3.5: `ScopedEngine` (RBAC ∩ jangkauan, ∪ delegasi),
hierarki OPD (`gov.org_units`, recursive CTE), resolver grant dari assignment role, resolver
delegasi aktif. Semuanya lulus unit test.

Dan tak satu pun dipakai. `RequirePermissionInUnit` punya **nol pemanggil produksi**, dan
`gateway.Context` memaknai evaluator yang tak dipasang sebagai **permisif** — jadi di server hidup
setiap pertanyaan "berwenang di unit ini?" dijawab YA, tanpa satu test pun berubah warna. Ini
bentuk kegagalan yang Sub-phase 5.0 ada untuk membayarnya (DoD 11): komponen benar, rakitan tak
ada, dan ketiadaannya tak bergejala.

Pertanyaan yang harus dijawab sebelum memasangnya: **siapa yang memakainya, dan dengan aturan
apa?** Memasang evaluator lalu menunggu pemanggil datang belakangan hanya memindahkan seam dorman
satu lapis ke dalam.

## Keputusan

**1. Pemanggil produksinya adalah use case yang MEMBERI wewenang.**
`tenantrole.AssignTenantRole` & `delegation.CreateDelegation` — dua-duanya menerima `UnitKerjaID`,
dan dua-duanya memberi orang lain kemampuan bertindak. Memegang `iam:tenant_role:assign` menjawab
"boleh menugaskan", **bukan** "boleh menugaskan di mana pun": tanpa lapis kedua, admin yang
wewenangnya dibatasi ke satu OPD bisa menugaskan role di OPD lain, lalu memanen wewenang itu lewat
akun yang ia kendalikan. Aturan yang sama dengan ADR-019, pada sumbu yang berbeda.

`SuratMasuk` sengaja **tidak** dipakai sebagai pemanggil pembuktian: ia tak punya `UnitKerjaID`,
dan menambahkannya semata agar DoD bisa ditunjukkan berarti memodelkan domain demi test.

**2. "Seluruh tenant" ditanyakan sebagai `AllowsInUnit(perm, uuid.Nil)`.**
`unit_kerja_id` kosong berarti jangkauan TERLUAS, jadi ia menuntut wewenang terluas — kalau tidak,
admin ber-scope satu OPD cukup **mengosongkan** field itu untuk menugaskan se-tenant. Eskalasi
lewat field yang dibiarkan kosong adalah bentuk yang paling sering lolos review, justru karena
tidak ada yang "ditulis" di sana.

Agar pertanyaan itu sahih, `uuid.Nil` tak boleh bisa menjadi jangkauan sebuah baris: `Validate`
pada kedua domain kini **menolak** `unit_kerja_id` ber-UUID nol dan mewajibkan `null`. Tanpa
invariant itu, satu baris ber-unit nol akan menjawab "ya" pada pertanyaan wewenang se-tenant tanpa
pernah memberi wewenang se-tenant kepada siapa pun.

Alternatif "tambah method `IsTenantWide()` ke port" ditolak: ia menambah kontrak lintas layer untuk
pertanyaan yang sudah bisa diajukan lewat kontrak yang ada, dan `Grant.TenantWide` sudah menjadi
satu-satunya yang menutupi `uuid.Nil`. Konvensi ini hidup di SATU tempat
(`permission.RequireAuthorityOver`), bukan disalin ke tiap use case.

**2b. `include_subtree` adalah pertanyaan TERSENDIRI, bukan varian dari yang di atas.**
Memberi `include_subtree` atas sebuah unit berarti memberi jangkauan atas SELURUH KETURUNANNYA.
Karena itu ia menuntut wewenang yang menjangkau keturunan itu — bukan wewenang atas unit itu saja.
`port.ScopedEvaluator` mendapat `AllowsSubtree` (dan `port.AuthContext` mendapat
`RequirePermissionInSubtree`) yang hanya dijawab ya oleh grant `TenantWide` atau grant ber-`Subtree`
atas unit itu/leluhurnya.

Tanpa cabang ini, pemegang wewenang atas satu unit SAJA bisa menerbitkan assignment/delegasi
ber-`include_subtree` pada unitnya sendiri dan dengan begitu membagikan jangkauan atas turunan yang
ia tak punya — eskalasi yang sama diam-diamnya dengan mengosongkan `unit_kerja_id`, hanya lewat
sebuah boolean. Ia tak bisa ditiru dengan memanggil `AllowsInUnit` berulang: pertanyaannya berbeda,
dan grant yang terikat persis pada satu unit menutupi unit itu tapi bukan keturunannya.

Satu pemeriksaan `IsWithin` cukup (tanpa mengembangkan subtree) karena sifat tree: bila unit berada
di dalam subtree grant, seluruh keturunan unit juga di dalamnya.

**3. Role sentral → Grant `TenantWide`.**
Role sentral tak punya konsep unit kerja; yang membatasinya `tenant_scope`, dan itu **sudah**
ditegakkan saat login (hanya role sentral yang berlaku untuk tenant token yang dibakar ke klaim —
ADR-019 Keputusan 3). Jadi setiap role sentral yang sampai ke evaluator berlaku penuh di tenant
ini. Menerjemahkannya jadi grant unit-scoped akan salah ke dua arah: helpdesk kementerian mendadak
butuh assignment unit, dan wewenang lintas-unit (inspektorat, PRD §hierarki "edge matriks") kehilangan
satu-satunya cara dinyatakan.

**4. Authority dirakit LAZY per request, di-memo termasuk kegagalannya.**
Authority menuntut dua query tenant DB (assignment role + delegasi aktif), sementara mayoritas
request tak pernah memanggil `RequirePermissionInUnit`. Merakit eager membebani SETIAP request
untuk kemampuan yang dipakai sebagian, tepat di jalur terpanas. `AllowsInUnit` sudah mengembalikan
error, jadi menunda tak menambah bentuk kegagalan baru. Kegagalan resolver menjadi **error**, bukan
`false`: "tak bisa memastikan" harus bisa dibedakan dari "tidak berwenang", dan tak pernah menjadi
"boleh".

**4b. Delegasi punya larangan MINIMUM yang tak boleh kosong (`DefaultNonDelegable`).**
`identity:*` dan `iam:*` non-delegable di semua tenant. Alasannya bukan kehati-hatian umum:
delegasi adalah jalur MANDIRI di evaluator dan pembuatnya belum diwajibkan memegang sendiri
permission yang ia limpahkan (lihat Keputusan tertunda). Dengan himpunan kosong, `identity:*` —
yang sudah dipagari agar tak bisa masuk role tenant — cukup "didelegasikan" untuk mendapat hasil
akhir yang sama (menerbitkan kredensial bagi person mana pun, lalu login sebagai orang itu), dan
`iam:*` melimpahkan kemampuan MELIMPAHKAN yang sekali lolos bisa dilebarkan berantai.

Entri berbentuk namespace (`ns:*`), bukan daftar permission utuh, supaya permission baru di keluarga
yang dilarang tidak diam-diam menjadi boleh didelegasikan — larangan yang bocor seiring waktu tanpa
ada yang mengubahnya.

**5. Konteks tanpa tenant mendapat Authority KOSONG — bukan evaluator nil.**
Citizen dan token sementara (sebelum pemilihan tenant) tetap dipasangi evaluator yang menolak
segalanya. Nil berarti permisif di `gateway.Context`, dan permisif di sini berarti seluruh scope
ABAC terbuka untuk konteks yang justru paling sedikit terverifikasi. Grant sentral pun tak
diemisikan di jalur ini: `TenantWide` tak punya arti bila tenant-nya belum ditentukan.

**6. Containment ISI: permission `iam:*` hanya boleh DIMASUKKAN ke role tenant oleh pembuat yang
memegangnya sendiri.**
Keputusan 1–2b menjaga **di mana** wewenang boleh diberikan. Ia tidak menjawab **apa** yang boleh
diberikan — dan lubang itu membatalkan sebagian yang baru saja dipagari: pemegang
`iam:tenant_role:buat` + `iam:tenant_role:assign` dapat mencetak role berisi `iam:delegasi:buat`
(permission yang tak pernah diberikan kepadanya), menugaskannya kepada dirinya sendiri **di dalam
unitnya** — lolos containment, karena unitnya memang unitnya — lalu memakainya. Efek sampingnya
lebih jauh: larangan `iam:*` pada delegasi (Keputusan 4b) menjadi dekorasi, sebab yang dilarang lewat
pintu delegasi tinggal dicetak lewat pintu role.

Karena itu `CreateTenantRole` menuntut `RequirePermission(p)` untuk setiap `p` ber-prefiks `iam:`
yang dimasukkan ke role. Pemeriksaannya RBAC biasa, bukan ber-scope: definisi role tak punya unit —
unit baru muncul saat penugasan, dan di sanalah Keputusan 1–2b bekerja. Pertanyaannya murni "apakah
kau memegang ini sama sekali?".

Pagarnya SENGAJA lebih longgar dari `identity:*` (yang dilarang mutlak di role tenant,
`tenantrole/domain.reservedPermissionPrefix`): `iam:*` adalah permission tenant yang sah — admin
tenant harus bisa menyusun role IAM untuk wakilnya — tapi hanya sejauh wewenang yang ia pegang
sendiri. Melarangnya mutlak akan memaksa setiap penunjukan admin IAM tenant melewati admin platform.

Dan SENGAJA tidak diperluas ke permission bisnis: mewajibkan pembuat role memegang
`keuangan:spm:terbitkan` sebelum boleh membuat role bendahara berarti administrasi role berhenti
menjadi pekerjaan tersendiri — admin IAM harus lebih dulu menjadi bendahara. Residu yang diterima
dicatat di Konsekuensi.

## Konsekuensi
- **Perubahan perilaku:** `RequirePermissionInUnit` berhenti permisif untuk request ber-token. Hari
  ini pemanggilnya dua (penugasan role & delegasi), jadi dampaknya terbatas dan disengaja.
- `middleware.EvaluatorFactory.Build` kini mengembalikan **dua** evaluator (RBAC + scoped) dari satu
  panggilan. Dua factory terpisah akan mengundang perakitan yang menyimpang — dan penyimpangan di
  sini berarti RBAC dan ABAC menjawab dari katalog yang berbeda.
- Permukaan HTTP baru `/admin/iam/*` (role tenant, penugasan, delegasi) di router bisnis, di balik
  `RequireAuth` — bukan top mux seperti `/auth/*`.
- `db.TxConn` (Conn + `Begin`) lahir dari kebutuhan nyata: repo ber-transaksi (`TenantRoleRepo`,
  `AuditRepo`) sebelumnya memegang `*db.Pool` dan karena itu **tak bisa dipakai di jalur request
  multi-tenant sama sekali**. `TenantRoutingConn` kini merutekan transaksi juga.
- `gov.audit_logs` mendapat penulis produksi pertamanya, dan karenanya butuh ensure-on-write per
  tenant (lihat DB_CHANGELOG PR-W3b).
- **Gerbang RBAC di handler menilai KLAIM TOKEN saja**, jadi wewenang lewat delegasi tak terlihat
  di sana. Untuk permukaan `/admin/iam/*` itu benar — `iam:*` non-delegable (Keputusan 4b), jadi tak
  seorang pun bisa memegang permission grup ini lewat delegasi. Tapi polanya TIDAK bisa disalin
  begitu saja: permukaan yang permission-nya boleh didelegasikan akan menolak PLT di handler,
  sebelum jalur delegasi di use case pernah dievaluasi. Saat permukaan seperti itu lahir, gerbang
  handler-nya harus dipikir ulang (kandidat: gerbang berbasis scoped-evaluator sesudah body
  di-parse, karena unit sasaran baru diketahui dari body).
- **Pemeriksaan wewenang memicu DDL ensure-on-write di jalur otorisasi.** Resolver grant & hierarki
  memastikan skema `gov.*` pada setiap pemanggilan (pola ensure-on-write repo ini, karena tabel
  framework `gov.*` belum punya runner migrasi). Konsekuensinya: pengecekan unit pertama sebuah
  request membawa beberapa `CREATE ... IF NOT EXISTS`, dan pada tenant yang benar-benar baru dua
  request bersamaan bisa berbenturan (DDL Postgres tak sepenuhnya atomik) → 500 sporadis di jalur
  otorisasi. Ini sifat pola yang sudah dipakai seluruh `gov.*`, bukan yang lahir di sini; solusi
  sesungguhnya adalah runner migrasi framework-gov yang sudah ter-DEFERRED di ROADMAP. Tidak
  ditambal setempat karena tambalan per-adapter justru menyembunyikan pola yang perlu diganti.
- **RESIDU YANG DITERIMA: `iam:tenant_role:buat` + `iam:tenant_role:assign` = wewenang setingkat
  admin tenant.** Siapa pun yang memegang pasangan itu dapat mencetak role berisi permission BISNIS
  apa pun (`keuangan:spm:terbitkan`, …) dan menugaskannya kepada dirinya sendiri dalam jangkauan
  unitnya — jadi ia efektif memegang seluruh permission bisnis tenant di dalam jangkauan itu. Ini
  sifat administrasi role, bukan cacat yang bisa ditambal tanpa mematikan administrasi role
  (Keputusan 6). Konsekuensi operasionalnya harus dinyatakan, bukan disimpan: pasangan permission itu
  **tidak boleh dibagikan seringan permission bisnis**, dan jangkauan unit-nya adalah satu-satunya
  yang membatasi dampaknya. Kandidat pagar berikutnya bila kelak dibutuhkan: permission ber-flag
  `grantable_by` di manifest modul, sehingga "apa yang boleh dimasukkan ke role" berhenti menjadi
  aturan prefiks dan menjadi properti permission itu sendiri.
- **Pemanggil di luar jalur HTTP tidak otomatis terlindungi.** CLI, importer, dan workflow action
  yang memanggil use case ini WAJIB menyediakan konteks ber-`ScopedEvaluator`; tanpa itu aturan ini
  diam-diam tak menegakkan apa pun. Sama seperti gerbang containment ADR-019, ia PER USE CASE.

## Alternatif yang dipertimbangkan
- **Menegakkan scope di lapis repository** (mis. `WHERE unit_kerja_id IN (jangkauan)`). Menutup
  lebih banyak jalur sekaligus, tapi memindahkan kebijakan otorisasi ke SQL tiap repo — tersebar,
  tak teruji sebagai aturan, dan mustahil dijawab "kenapa ditolak". Ditolak.
- **Menegakkan di handler HTTP saja.** Lebih murah, tapi meninggalkan use case telanjang bagi
  pemanggil non-HTTP — persis lubang yang membuat `CreateCredential` perlu diperbaiki di W3a.
- **Menolak `include_subtree` kecuali actor berwenang se-tenant** (alih-alih menambah
  `AllowsSubtree`). Nol perubahan port dan fail-closed, tapi over-restriktif dengan cara yang
  merugikan: kepala OPD yang memang berwenang atas subtree-nya kehilangan satu-satunya bentuk
  penugasan yang wajar, dan jalan yang tersisa baginya adalah penugasan SE-TENANT — lebih longgar
  dari yang ingin dicegah. Ditolak.
- **`ScopedEvaluator` eager di middleware.** Sederhana dan bebas memo, tapi dua query tenant DB per
  request untuk kemampuan yang jarang dipakai. Ditolak (Keputusan 4).
- **Menormalkan `unit_kerja_id` kosong menjadi `uuid.Nil` di DTO.** Menghapus pointer dan terasa
  rapi, tapi menghapus juga bedanya "tidak disebut" dan "disebut nol" — yaitu tepat perbedaan yang
  menjadi dasar Keputusan 2.

## Keputusan tertunda
- **Delegasi: pembuat belum diwajibkan MEMEGANG tiap permission yang ia limpahkan.** Yang diperiksa
  jangkauan unit; `NonDelegableSet` menutup permission paling berbahaya. Pemeriksaan per-permission
  menuntut evaluasi wewenang DELEGATOR (bukan pembuat) — konsep yang belum ada di evaluator.
- **Pagar isi masih berbasis PREFIKS (`iam:`), bukan properti permission.** Modul yang kelak
  memperkenalkan permission "pemberi wewenang" di namespace-nya sendiri tak otomatis terpagari.
  Bentuk akhirnya adalah flag di manifest modul (lihat Konsekuensi); prefiks dipilih sekarang karena
  ia menutup seluruh keluarga yang ADA hari ini tanpa menunggu perubahan manifest.
- **Containment untuk PENCABUTAN.** Use case revoke role tenant/delegasi belum ada; saat lahir ia
  harus melewati gerbang yang sama (mencabut wewenang orang lain adalah mutasi wewenang juga).
- **`unit_kerja_id` belum ber-FK ke `gov.org_units`.** Unit yang tak ada bisa disebut; ia hanya tak
  menjangkau apa pun (fail-closed), tapi FK formal menunggu runner migrasi framework-gov.
