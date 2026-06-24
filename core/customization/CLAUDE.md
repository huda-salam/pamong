# core/customization — Tenant Customization Layer

Layer kustomisasi tenant yang TERPISAH dari definisi modul inti. Custom field, label
override, capability flags. Upgrade framework tidak menimpa kustomisasi tenant; sebaliknya
kustomisasi tenant tidak mengotori modul inti. Dipelajari dari Custom Field ERPNext.

## Bergantung pada
- port/ + stdlib; core/domain (entity yang dikustomisasi)

## Tanggung jawab
- Custom field per-tenant (tambah field tanpa ubah modul inti)
- Label/tampilan override per-tenant
- Capability flags per-tenant (gate fitur dormant)
- Penyimpanan terpisah (gov.tenant_customizations), upgrade-safe

## File kunci
- customfield.go — definisi & penyimpanan custom field
- override.go — label/tampilan override
- capability.go — capability flags
- merge.go — gabungkan definisi inti + kustomisasi tenant saat runtime

## Konvensi khusus
- Kustomisasi hidup di layer terpisah, di-merge dengan definisi modul saat runtime.
- Capability flag mengaktifkan fitur dormant tanpa rilis terpisah / percabangan kode.

## Pitfall umum
- Menaruh kustomisasi tenant di tabel modul inti -> rusak saat upgrade.
- Custom field yang bentrok dengan field inti -> namespace/validasi.

## Test
- Unit: merge definisi inti + custom field, override label, capability on/off.
- go test ./core/customization/... -race

## Rujukan
- PRD.md, core/domain/PRD.md, CODING_PHILOSOPHY.md #6
