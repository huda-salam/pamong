//go:build integration

package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/huda-salam/pamong/infra/storage"
	"github.com/huda-salam/pamong/infra/storage/drivers"
	"github.com/huda-salam/pamong/port"
)

// newMinIO membuat driver MinIO dari env test dan memastikan bucket ada. Test
// di-skip bila PAMONG_TEST_MINIO_ENDPOINT tidak diset.
//
// Menjalankan MinIO lokal (via `sg docker -c`):
//
//	sg docker -c 'docker run -d --name pamong-minio -p 9000:9000 \
//	  -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin \
//	  minio/minio server /data'
//	export PAMONG_TEST_MINIO_ENDPOINT=http://127.0.0.1:9000
//	export PAMONG_TEST_MINIO_ACCESS_KEY=minioadmin
//	export PAMONG_TEST_MINIO_SECRET_KEY=minioadmin
//	go test ./infra/storage/... -tags=integration
func newMinIO(t *testing.T) *drivers.MinIO {
	t.Helper()
	endpoint := os.Getenv("PAMONG_TEST_MINIO_ENDPOINT")
	if endpoint == "" {
		t.Skip("PAMONG_TEST_MINIO_ENDPOINT tidak diset — lewati integration test")
	}
	d, err := drivers.NewMinIO(drivers.MinIOConfig{
		Endpoint:  endpoint,
		Bucket:    "pamong-test",
		AccessKey: os.Getenv("PAMONG_TEST_MINIO_ACCESS_KEY"),
		SecretKey: os.Getenv("PAMONG_TEST_MINIO_SECRET_KEY"),
	})
	if err != nil {
		t.Fatalf("NewMinIO: %v", err)
	}
	if err := d.EnsureBucket(context.Background()); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}
	return d
}

func upload(t *testing.T, s port.StoragePort, key, body string) {
	t.Helper()
	if err := s.Upload(context.Background(), key, bytes.NewReader([]byte(body)), port.StorageMeta{
		ContentType: "text/plain",
		TenantID:    "pemkot-surabaya",
		Module:      "surat_masuk",
	}); err != nil {
		t.Fatalf("upload %q: %v", key, err)
	}
	t.Cleanup(func() { _ = s.Delete(context.Background(), key) })
}

// TestMinIO_UploadDownload_KontenIdentik: DoD PRD — upload lalu download dari MinIO
// menghasilkan konten identik.
func TestMinIO_UploadDownload_KontenIdentik(t *testing.T) {
	s := newMinIO(t)
	key := storage.BuildKey(port.StorageMeta{TenantID: "pemkot-surabaya", Module: "surat_masuk", EntityID: "1"}, "scan.pdf")
	upload(t, s, key, "isi dokumen surat")

	rc, err := s.Download(context.Background(), key)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "isi dokumen surat" {
		t.Errorf("konten: mau %q dapat %q", "isi dokumen surat", string(got))
	}
}

// TestMinIO_IsolasiKeyPerTenant: key ter-namespace per tenant; List berprefix tenant
// hanya melihat objek tenant tersebut.
func TestMinIO_IsolasiKeyPerTenant(t *testing.T) {
	s := newMinIO(t)
	keyA := storage.BuildKey(port.StorageMeta{TenantID: "tenant-a", Module: "m", EntityID: "1"}, "f.txt")
	keyB := storage.BuildKey(port.StorageMeta{TenantID: "tenant-b", Module: "m", EntityID: "1"}, "f.txt")
	upload(t, s, keyA, "punya A")
	upload(t, s, keyB, "punya B")

	keys, err := s.List(context.Background(), "tenant-a/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 || keys[0] != keyA {
		t.Fatalf("list tenant-a: mau [%q] dapat %v", keyA, keys)
	}
}

func TestMinIO_DownloadNotFound(t *testing.T) {
	s := newMinIO(t)
	_, err := s.Download(context.Background(), "tenant-x/tidak/ada.txt")
	if !errors.Is(err, port.ErrObjectNotFound) {
		t.Errorf("mau ErrObjectNotFound, dapat %v", err)
	}
}

// TestMinIO_GantiDriverTanpaUbahPemanggil membuktikan driver minio memenuhi
// port.StoragePort yang sama dengan local (PRD AC: ganti driver tanpa ubah pemanggil).
func TestMinIO_GantiDriverTanpaUbahPemanggil(t *testing.T) {
	var _ port.StoragePort = newMinIO(t)
}
