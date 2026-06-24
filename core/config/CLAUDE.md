# core/config — Configuration & Tenant Config

Dua hal: (1) konfigurasi aplikasi berlapis (env > local > env-file > default), dan
(2) tenant config ber-scope bertingkat (tenant[/unit_kerja[/resource]]) dengan resolusi
"paling spesifik menang". Modul TIDAK baca env langsung.

## Bergantung pada
- port/ + stdlib

## Tidak boleh
- Modul memanggil os.Getenv langsung [linter: config-no-direct-env]

## Tanggung jawab
- Load config aplikasi: env GOV_*, file YAML berlapis, precedence, validasi
- AppConfig struct yang di-inject ke modul saat Bootstrap
- Tenant config ber-scope: penyimpanan & resolver bertingkat
- Config field tambahan modul (dari manifest ConfigSpec)
- Versioning config yang berdampak (lewat pola versioned + effective date)

## File kunci
- loader.go — load & merge config berlapis
- schema.go — AppConfig struct + validasi
- tenant_config.go — penyimpanan tenant config (gov.tenant_configs)
- resolver.go — resolusi scope bertingkat (paling spesifik menang)

## Konvensi khusus
- Env prefix GOV_, format GOV_{SECTION}_{KEY}.
- Tenant config key ber-scope: tenant_id[/unit_kerja_id[/resource_id]].
- Resolver: cari paling spesifik dulu, fallback ke lebih umum.
- Config yang berdampak akuntansi (mis. pilihan strategy) ber-versi + effective date.

## Pitfall umum
- Modul baca os.Getenv -> dilarang; terima lewat AppConfig.
- Mengasumsikan scope selalu tenant -> resolver harus dukung unit/resource.

## Test
- Unit: precedence (env > local > env-file > default), resolusi scope bertingkat.
- go test ./core/config/... -race

## Rujukan
- PRD.md, core/strategy/PRD.md (pemakai utama tenant config)
