package drivers

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/huda-salam/pamong/port"
)

// MinIO adalah driver S3-compatible untuk produksi (PRD storage F2), berlaku untuk
// MinIO maupun AWS S3 lewat client minio-go. Objek disimpan pada satu bucket; key
// menjadi object name. Isolasi antar tenant ditegakkan lewat prefix key
// (storage.BuildKey), bukan bucket per tenant.
type MinIO struct {
	client *minio.Client
	bucket string
}

// MinIOConfig mengonfigurasi driver minio/s3. Secure (TLS) diturunkan dari skema
// Endpoint (https:// → TLS aktif).
type MinIOConfig struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
}

var _ port.StoragePort = (*MinIO)(nil)

// NewMinIO membuat client S3-compatible. Tidak membuka koneksi (minio-go lazy);
// gunakan EnsureBucket untuk memverifikasi konektivitas & keberadaan bucket.
func NewMinIO(cfg MinIOConfig) (*MinIO, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("storage/minio: endpoint (GOV_STORAGE_ENDPOINT) wajib diisi")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("storage/minio: bucket (GOV_STORAGE_BUCKET) wajib diisi")
	}
	host, secure := parseEndpoint(cfg.Endpoint)
	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("storage/minio: buat client %q: %w", host, err)
	}
	return &MinIO{client: client, bucket: cfg.Bucket}, nil
}

// parseEndpoint memisahkan host:port dari skema URL. minio-go menerima host tanpa
// skema plus flag Secure terpisah.
func parseEndpoint(raw string) (host string, secure bool) {
	switch {
	case strings.HasPrefix(raw, "https://"):
		return strings.TrimPrefix(raw, "https://"), true
	case strings.HasPrefix(raw, "http://"):
		return strings.TrimPrefix(raw, "http://"), false
	default:
		return raw, false
	}
}

// EnsureBucket membuat bucket bila belum ada (idempoten). Dipanggil saat wiring
// bootstrap atau setup test — bukan di setiap Upload agar hemat round-trip.
func (m *MinIO) EnsureBucket(ctx context.Context) error {
	exists, err := m.client.BucketExists(ctx, m.bucket)
	if err != nil {
		return fmt.Errorf("storage/minio: cek bucket %q: %w", m.bucket, err)
	}
	if !exists {
		if err := m.client.MakeBucket(ctx, m.bucket, minio.MakeBucketOptions{}); err != nil {
			return fmt.Errorf("storage/minio: buat bucket %q: %w", m.bucket, err)
		}
	}
	return nil
}

// Upload menstream r ke objek key. objectSize -1 = panjang tak diketahui; minio-go
// mem-buffer per part, tidak memuat seluruh isi ke memori (PRD NFR). Metadata
// tenant/module/entity disimpan sebagai user-metadata untuk telusur.
func (m *MinIO) Upload(ctx context.Context, key string, r io.Reader, meta port.StorageMeta) error {
	opts := minio.PutObjectOptions{ContentType: meta.ContentType}
	user := map[string]string{}
	if meta.TenantID != "" {
		user["tenant"] = meta.TenantID
	}
	if meta.Module != "" {
		user["module"] = meta.Module
	}
	if meta.EntityID != "" {
		user["entity-id"] = meta.EntityID
	}
	if len(user) > 0 {
		opts.UserMetadata = user
	}
	if _, err := m.client.PutObject(ctx, m.bucket, key, r, -1, opts); err != nil {
		return fmt.Errorf("storage/minio: upload %q: %w", key, err)
	}
	return nil
}

// Download mengembalikan reader objek key. GetObject bersifat lazy, jadi eksistensi
// diverifikasi lewat Stat agar not-found tersurface saat ini juga sebagai
// port.ErrObjectNotFound.
func (m *MinIO) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := m.client.GetObject(ctx, m.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage/minio: download %q: %w", key, err)
	}
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, fmt.Errorf("storage/minio: %q: %w", key, port.ErrObjectNotFound)
		}
		return nil, fmt.Errorf("storage/minio: stat %q: %w", key, err)
	}
	return obj, nil
}

// Delete menghapus objek key. RemoveObject S3 idempoten: key tak ada bukan error.
func (m *MinIO) Delete(ctx context.Context, key string) error {
	if err := m.client.RemoveObject(ctx, m.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage/minio: hapus %q: %w", key, err)
	}
	return nil
}

// List mengembalikan seluruh object key berawalan prefix (rekursif). Iterasi channel
// minio-go; error per-objek dihentikan dan dikembalikan.
func (m *MinIO) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for obj := range m.client.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("storage/minio: list prefix %q: %w", prefix, obj.Err)
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}
