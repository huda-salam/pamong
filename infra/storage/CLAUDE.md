# infra/storage — File Storage Adapter

Driven adapter: implementasi port.StoragePort. Driver MinIO/S3-compat, local (dev/test).
Untuk lampiran entity (HasAttachments) dan dokumen.

## Bergantung pada
- port/storage.go; pustaka S3/MinIO client

## Tanggung jawab
- Upload/download/delete/list dengan metadata
- Driver: minio/s3 (produksi), local (dev/test)
- Key namespacing per tenant/module/entity

## File kunci
- storage.go — entry; drivers/minio.go, drivers/local.go

## Konvensi khusus
- Key: {tenant}/{module}/{entity}/{id}/{filename}. Isolasi per tenant.
- Permission lampiran mengikuti entity induk (dicek di use case/gateway, bukan di sini).

## Test
- Integration (MinIO): upload & download; isolasi key per tenant.
- go test ./infra/storage/... -tags=integration

## Rujukan
- PRD.md, port/storage.go, core/domain (HasAttachments)
