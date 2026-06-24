# identity/ — Identity Module

Modul sentral identitas: person, employment, credential, central role, tenant assignment,
auth flow. DB TERPISAH (gov_identity). Data di-clone ke tenant via event. SENSITIF —
setiap perubahan butuh review ekstra (lihat aturan PR).

## Bergantung pada
- port/, core/domain, core/permission

## Tidak boleh diimport modul bisnis
- Akses dari modul bisnis HANYA lewat port (UserResolver) + event. Import package
  identity/ dari modul bisnis -> linter tolak.

## Tanggung jawab
- Model: person (anchor NIK), employment (opsional, NIP untuk ASN), credential (banyak)
- Persona: konteks login (employee | citizen) — BUKAN tipe orang
- Central role: global (semua tenant) + scoped (tenant_scope[])
- Tenant assignment: penugasan employment ke tenant; cross-tenant (PLT/PJ) ber-otorisasi
- Auth flow: tiga jalur (employee sentral, employee daerah, citizen publik)
- JWT issue/verify, revocation (jti)
- Sync engine: clone person/employment ke tenant DB via event

## BUKAN tanggung jawab
- Evaluasi permission (itu core/permission; identity menyimpan central role master)
- Data operasional tenant (modul bisnis)

## File kunci
- domain/ — person, employment, credential, central role, assignment + ports
- usecase/ — create person, attach employment, assign role, cross-tenant assign, login
- adapter/ — http (auth endpoints), db (identity DB)
- sync/ — clone engine (subscribe event, tulis ke tenant DB)

## Konvensi khusus
- ASN = masyarakat yang punya employment. Bisa login publik sebagai citizen (token tanpa
  role internal). Persona ditentukan portal, bukan tipe orang.
- NIK anchor global unik. NIP di employment (unik, wajib untuk ASN).
- Cross-tenant assignment (is_home_tenant=false) butuh permission khusus.
- Identity DB selalu sentral; tenant (termasuk dedicated server) tetap connect untuk auth.

## Pitfall umum
- Memodelkan user_type sebagai properti person (SALAH). Yang ada: employment (opsional)
  + persona (konteks login).
- Mengira citizen butuh tenant assignment (TIDAK). Hanya employee yang butuh.
- ASN login publik membawa role internal (SALAH, harus tanpa role internal).

## Test
- Unit: resolve by NIK/NIP, persona resolution, central role scope, cross-tenant otorisasi.
- Integration: clone sync via event -> tenant punya data user.
- go test ./identity/... -race

## Rujukan
- PRD.md, core/permission/PRD.md, port/user.go, port/auth.go
