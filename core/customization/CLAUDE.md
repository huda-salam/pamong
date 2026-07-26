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
- customfield.go — CustomFieldDef, DataClass, CustomFieldStore + Memory impl
- merge.go — MergeEntity (inti + custom aktif) + ValidateAgainstBase + EntityLookup
- label.go — label override numpang di config ber-scope (LabelKey + LabelResolver)
- capability.go — capability flags (registry + resolver + store)
- admin.go — Manager: jalur tulis ber-permission + event (custom field, label, capability)
- events.go / permissions.go — konstanta event & permission kustomisasi
- migrations/ — gov.tenant_custom_fields, gov.tenant_capability_overrides (label pakai gov.tenant_configs)
- (infra) infra/customization/ — adapter Postgres CustomFieldStore & TenantCapabilityStore

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
