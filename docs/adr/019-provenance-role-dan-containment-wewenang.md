# ADR-019: Provenance role & containment wewenang pada mutasi identitas

## Status
Accepted (2026-08-11). Dikerjakan di PR-W3a. Menutup REVIEW_BACKLOG **B7** dan **B8**.

Mengubah kontrak `port.PermissionEvaluator` (dari nama role telanjang ke `port.RoleRef`) dan
bentuk `core/permission.CompositeCatalog` / `Authority` — karena itu ia ADR, bukan tambalan.

## Konteks

Dua temuan review terbuka ternyata satu cacat yang dilihat dari dua sisi: **wewenang tidak
dibawa sampai ke titik keputusan.**

**B8 — lapis asal role hilang.** Otorisasi di-resolve dari NAMA role. `gateway.Context`
meratakan klaim `tenant_roles` dan `central_roles` jadi satu daftar nama, lalu
`CompositeCatalog.Lookup` mencoba katalog central lebih dulu. Akibatnya role TENANT yang
kebetulan — atau sengaja — bernama sama dengan role sentral me-resolve ke DEFINISI SENTRAL:
mewarisi permission-nya sekaligus `LayerGlobal`, yang menang tanpa syarat dan melewati
strict-intersection. Kedua regex nama sebentuk (`^[a-z][a-z0-9_]{2,99}$`), jadi `super_admin`
adalah nama role tenant yang sah. Konsekuensinya B6 (reservasi namespace `identity:` bagi role
tenant) **bisa dilewati tanpa menyebut `identity:` sama sekali**: admin tenant membuat role
tenant bernama persis seperti role sentral, daftar permission-nya boleh kosong, menugaskannya ke
diri sendiri, login ulang → seluruh permission sentral terbuka.

**B7 — scope aktor hilang.** Ketiga mutasi identitas (`CreateCredential`,
`AssignEmploymentToTenant`, `AssignCentralRole`) memeriksa **apakah aktor punya permission**, tak
pernah **apakah TARGET berada dalam wewenang aktor**. Scope hanya disaring saat LOGIN
(`CentralRoleResolver` membangun klaim), lalu tak pernah diperiksa lagi terhadap sasaran satu
operasi. Yang terkuat adalah `CreateCredential`: `UNIQUE`-nya `(cred_type, cred_value)` — bukan
`person_id` — jadi kredensial TAMBAHAN untuk orang yang sudah punya tetap berhasil, dan login
me-resolve murni lewat `(cred_type, cred_value) → person_id`. Menerbitkan kredensial karena itu
SETARA dengan menjadi target.

Dirantai, keduanya menjadi pengambilalihan platform oleh satu tenant.

Ada tiga pertanyaan kebijakan yang menggantung dan membuat B7 tak layak ditambal: bolehkah
menugaskan ke tenant selain tenant token? bolehkah memberikan role yang tak dipegang sendiri?
bolehkah menerbitkan kredensial untuk orang yang wewenangnya melampaui wewenang penerbit?

## Keputusan

**1. Lapis asal role dibawa sampai ke titik evaluasi; resolusi dikurung per lapis.**

`port.RoleRef{Origin, Name}` menggantikan nama telanjang sebagai masukan
`PermissionEvaluator.Allows`. Nama dari klaim `tenant_roles` dicari HANYA di katalog tenant,
nama dari `central_roles` HANYA di katalog central. Nama yang sama di dua lapis kini adalah
**dua role yang berbeda**.

Ini bukan menambah data, melainkan **berhenti membuangnya**: JWT sudah memisahkan kedua klaim
sejak PR-2.4.1. Yang diperbaiki adalah perataan di `gateway/context.go`.

Layer hasil lookup **dijepit** ke lapis asal (apa pun yang dilaporkan katalog tenant
diperlakukan `LayerTenant`) — pertahanan berlapis, supaya katalog tenant mana pun (adapter baru,
cache, katalog uji) tak bisa menaikkan lapis walau salah menulisnya.

**2. Mutasi identitas menegakkan containment aktor→target, dengan satu pintu keluar eksplisit.**

Aturannya diambil apa adanya dari Kubernetes RBAC (*privilege escalation prevention*): seseorang
hanya boleh MEMBUAT wewenang yang ia sendiri sudah pegang, pada scope yang sama — atau ia diberi
verb `escalate` secara eksplisit. Terjemahannya:

| # | Aturan | Ditegakkan di |
|---|---|---|
| 1 | Operasi yang menyebut tenant hanya boleh menyebut **tenant token aktor** | `AssignEmploymentToTenant` |
| 2 | Role sentral hanya boleh diberikan bila aktor memegang **seluruh** permission yang role itu berikan, dan scope-nya = tenant aktor; role **global** selalu di luar wewenang | `AssignCentralRole` |
| 3 | Kredensial hanya boleh diterbitkan untuk person yang **wewenang sentralnya tidak melampaui** wewenang aktor | `CreateCredential` |

Pintu keluarnya satu dan eksplisit: **`identity:authority:escalate`**. Ia permission TERSENDIRI,
bukan pengecualian tersembunyi di dalam `identity:central_role:assign` / `identity:credential:buat`
— persis alasan k8s memisahkan verb `escalate` dari `create`: kalau pintu keluar menyatu dengan
izin biasa, tak ada satu pun kueri audit yang bisa menjawab "siapa yang boleh melampaui
wewenangnya". Karena berada di namespace `identity:`, ia tak bisa datang dari role tenant (B6).

Kepemilikan permission aktor diperiksa lewat `ctx.RequirePermission`, yakni lewat Engine +
katalog yang **sama** dengan jalur otorisasi biasa — jadi resolusi konflik penuh dan pengurungan
lapis asal (Keputusan 1) ikut berlaku tanpa diulang. Pemeriksaan yang punya jalur evaluasinya
sendiri akan menyimpang dari otorisasi sesungguhnya diam-diam.

**3. Wewenang teritorial aktor = `ctx.TenantID()`, bukan klaim `tenant_scope`.**

Token Pamong SELALU ter-scope ke tepat satu tenant, dan role sentral di dalamnya SUDAH disaring
untuk tenant itu saat login (PR-2.4.3); klaim `tenant_scope` karena itu sengaja diterbitkan
KOSONG. Menambahkan `TenantScope()` ke `port.AuthContext` hanya untuk membaca daftar yang selalu
kosong berarti memasang kontrak dorman lintas layer — pola yang justru dibayar Sub-phase 5.0.
Bila kelak token multi-tenant diterbitkan, yang berubah cukup satu fungsi
(`requireTenantWithinAuthority`).

Konteks tanpa tenant (citizen, job, konteks uji) **tidak** berlaku sebagai wildcard: ia gagal
containment. Justru konteks itulah yang paling tak punya dasar teritorial.

Dua sumbu penyaringan pada aturan 3 sengaja LONGGAR, karena artefak yang diotorisasi —
kredensial — berumur permanen sementara tiap penyaringan hanya memotret satu titik:

- **Tenant-agnostik.** Role target yang scoped ke tenant LAIN tetap dihitung; kredensial tidak
  terikat tenant, jadi siapa pun yang bisa login sebagai target dapat memilih tenant itu
  sesudahnya (`POST /auth/select-tenant`).
- **Waktu-agnostik ke depan.** Assignment yang belum mulai tetap dihitung; `valid_from` datang
  dari klien, jadi role global yang dijadwalkan pekan depan akan tampak "tidak aktif" hari ini —
  kredensialnya terbit sekarang, wewenangnya dipanen nanti. Hanya `ValidUntil` yang sudah lewat
  yang aman diabaikan, sebab wewenang itu tak akan kembali.

**4. Ditegakkan di PINTU TULIS, bukan di authorizer.**

Sama seperti k8s menegakkan `escalate` di API server, dan sama seperti B6 sudah ditutup di repo
ini (`TenantRole.Validate` dipanggil dari `Save`). Authorizer tetap menjawab "punya permission?";
"boleh atas target ini?" dijawab use case yang memegang target.

**5. Pemegang pertama `identity:authority:escalate` di-SEED, tidak bisa lahir dari dalam aplikasi.**

Memberikan role yang memuat `escalate` menuntut aktor sudah memegangnya (Keputusan 2), jadi
tidak ada jalur in-app untuk mencetak pemegang PERTAMA. Itu disengaja dan bukan kebuntuan: ia
mengikuti jalur bootstrap yang sudah ada di repo ini — admin platform pertama pun tak bisa
dibuat lewat API (`assigned_by` NOT NULL ber-FK, dijawab sentinel `domain.SystemActorID` dari
migrasi 010). Genesis wewenang selalu di luar sistem yang mengaturnya; Kubernetes melakukan hal
yang sama dengan `system:masters` yang tertanam di sertifikat cluster, bukan di-grant lewat API.

Mekanismenya konkret: role sentral bootstrap diseed lewat repo/SQL bersama admin pertama, dan
daftar permission-nya WAJIB memuat `identity:authority:escalate` — kalau tidak, instalasi baru
punya admin yang tak bisa menugaskan role global maupun menugaskan lintas tenant, tanpa jalan
keluar. Dicontohkan `seedAdminBootstrap` di `cmd/server/admin_identity_e2e_integration_test.go`.
**`pamongctl` belum punya perintah seed admin** — dicatat sebagai backlog eksplisit di ROADMAP,
bukan diasumsikan ada.

**6. Setiap aturan menuntut aktor terikat tenant, SEBELUM pintu keluar diperiksa.**

`gateway.Context.RequirePermission` mengembalikan nil bila tidak ada evaluator ter-wire (default
permisif warisan). Konteks tanpa evaluator karena itu terbaca "boleh eskalasi" — arah gagal yang
berlawanan dengan seluruh keputusan di atas. Hari ini jalur itu tak terjangkau lewat HTTP
(`RequireAuth` memagari `/admin/identity/*`, dan `evaluatorFactory.Build` tak pernah nil), dan
pada konteks semacam itu gerbang permission di ATAS use case pun sudah lolos — jadi ia tak
menambah lubang. Yang ia tambah adalah KETERGANTUNGAN: melonggarkan default permisif kelak akan
mematikan containment bersamaan, diam-diam.

Karena itu tiap aturan lebih dulu menuntut `ctx.TenantID()` tak kosong — sinyal POSITIF dari
klaim JWT tersigning lewat `TenantResolver`, yang tak bisa datang dari default permisif. Tak ada
aktor sah yang kehilangan apa pun: ketiga permission mutasi identitas hanya bisa datang dari role
sentral (B6), dan role sentral hanya berlaku bagi persona employee — yang tokennya selalu
ter-scope satu tenant.

Akar masalahnya (RequirePermission permisif saat evaluator nil) adalah perubahan postur repo-wide
dan dijadwalkan terpisah di ROADMAP, bukan ditambal di sini.

## Konsekuensi

- **Perubahan perilaku yang disengaja:** menugaskan role GLOBAL, menugaskan ke tenant lain, dan
  menerbitkan kredensial untuk pemegang role sentral yang lebih luas kini **403** kecuali aktor
  memegang `identity:authority:escalate`. Admin platform yang ada wajib diberi permission itu
  saat onboarding — dan pemberiannya sendiri kini terlihat di audit.
- **Dua kontrak port berubah** (`PermissionEvaluator.Allows`, dan `CompositeCatalog` berhenti
  mengimplementasi `RoleCatalog`). Yang terakhir disengaja: kompilasi adalah penegakannya —
  jalur lookup nama telanjang tak bisa dipanggil ulang tanpa terlihat.
- **`CreateCredential` mendapat dua dependensi** (repo role sentral + assignment) dan menolak
  keduanya nil saat perakitan. Sebuah kontrol keamanan yang menunggu pemanggil ingat memasangnya
  bukan kontrol.
- **Residu yang diketahui & disengaja:** aturan 3 memeriksa wewenang SENTRAL target. Role TENANT
  target hidup di DB tenant sementara use case identity hanya berbicara ke identity DB;
  memeriksanya menuntut port lintas-DB baru. Sisa risikonya lateral DI DALAM satu tenant, bukan
  pengambilalihan platform. Dicatat di REVIEW_BACKLOG, bukan diam-diam dianggap tertutup.
- **Satu permukaan sengaja TIDAK ikut:** `has_role()` di guard workflow tetap meratakan lapis,
  karena `AuthContext.HasRole` memang berkontrak "tenant ATAU central". Menutupnya menuntut
  perubahan kontrak port + semantik DSL guard, jadi ia keputusan tersendiri — dicatat sebagai
  REVIEW_BACKLOG **B9** dengan gerbang "sebelum pemda boleh menulis workflow sendiri". Guard yang
  menentukan wewenang sebaiknya memakai `has_permission`, yang melewati Engine dan karena itu
  sudah ber-lapis-asal.
- Biaya baca tambahan pada `CreateCredential`: satu `ListByPerson` + satu `FindByID` per role
  aktif target. Dibayar sebelum bcrypt, jadi permintaan yang ditolak justru lebih murah dari
  sebelumnya.

## Alternatif yang dipertimbangkan

**Menolak nama role tenant yang bertabrakan dengan katalog sentral, di pintu tulis.** Lebih
sempit, tapi (a) butuh port baru — katalog sentral hidup di identity DB sedangkan
`TenantRole.Validate` ada di domain tenant; (b) hanya menutup pembuatan BARU, tak menyentuh baris
yang terlanjur ada; (c) meninggalkan resolusi berbasis nama telanjang, yaitu akar cacatnya. Ia
mengunci pintu sambil membiarkan tembok berlubang.

**Mereservasi daftar nama (`super_admin` dkk) bagi lapis sentral.** Daftar hitam yang harus
diperbarui tiap role sentral baru lahir. Kegagalannya senyap.

**Menaikkan prioritas tenant di composite (tenant dulu, baru central).** Membalik arah cacat:
tenant jadi bisa men-*shadow* role global — yang justru diantisipasi komentar lama di
`composite.go`. Bukan perbaikan, hanya memindahkan lubang.

**Menegakkan containment di authorizer (`Engine`), bukan di use case.** Engine tak tahu TARGET
sebuah operasi — ia hanya melihat role dan permission. Memasukkan target ke sana berarti
memberinya pengetahuan domain identity dan membuat setiap pemanggil menerangkan sasarannya dalam
istilah permission. K8s juga tidak melakukannya di authorizer.

**Menambah `TenantScope()` ke `port.AuthContext` sesuai catatan awal B7.** Ditolak: nilainya
selalu kosong hari ini (lihat Keputusan 3), jadi ia kontrak lintas layer yang dorman sejak lahir.

**Menjadikan pintu keluar sebuah pengecualian di dalam permission yang sudah ada** (mis.
"pemegang `identity:central_role:assign` dari role global boleh apa saja"). Ditolak: pengecualian
tersembunyi tak bisa diaudit, dan ia mengembalikan keputusan otorisasi ke penebakan lapis role.

## Referensi
- REVIEW_BACKLOG B6 (jalur string), **B7**, **B8**
- <https://kubernetes.io/docs/reference/access-authn-authz/rbac/> — *Privilege escalation
  prevention and bootstrapping*
- Keycloak `realm_access.roles` vs `resource_access.{clientId}.roles` — container ikut jadi
  bagian rujukan role, cacat B8 tak mungkin terjadi di sana
- ADR-014 (deklarasi strict permission dari manifest), ADR-003 (audit mutasi identity)
