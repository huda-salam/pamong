# PRD: File Storage

## Tujuan
Menyimpan lampiran dan dokumen secara aman dan terisolasi per tenant, dengan abstraksi
yang memungkinkan ganti backend (MinIO/S3/local) tanpa ubah pemanggil.

## Kebutuhan fungsional
- F1: Upload/Download/Delete/List via StoragePort dengan metadata (content-type, module,
  entity, tenant).
- F2: Driver minio/s3 (produksi) & local (dev/test), dipilih config.
- F3: Key namespacing per tenant/module/entity untuk isolasi.
- F4: Mendukung entity HasAttachments (endpoint lampiran di gateway memakai port ini).

## Kebutuhan non-fungsional
- Streaming upload/download (io.Reader/Closer), tidak load seluruh file ke memori.
- Isolasi: key tenant A tidak dapat diakses lewat konteks tenant B.

## Dependency
- port/storage.go; S3/MinIO client; config (endpoint, bucket, kredensial).

## Anti-pattern
- Memuat file besar ke memori penuh. Key tanpa namespace tenant.

## Acceptance criteria
- [ ] Upload lalu download file dari MinIO menghasilkan konten identik.
- [ ] Key ter-namespace per tenant; isolasi terjaga.
- [ ] Ganti driver (local↔minio) tanpa ubah pemanggil.
