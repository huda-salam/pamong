package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/storage"
	"github.com/huda-salam/pamong/port"
)

// newLocal membuat StoragePort berdriver local dengan root di direktori sementara.
// Mengembalikan lewat port.StoragePort agar test membuktikan pemanggil tak
// bergantung pada driver konkret.
func newLocal(t *testing.T) port.StoragePort {
	t.Helper()
	s, err := storage.NewFromConfig(config.StorageConfig{
		Driver:   "local",
		Endpoint: t.TempDir(),
		Bucket:   "pamong",
	})
	if err != nil {
		t.Fatalf("NewFromConfig local: %v", err)
	}
	return s
}

func mustUpload(t *testing.T, s port.StoragePort, key, body string, meta port.StorageMeta) {
	t.Helper()
	if err := s.Upload(context.Background(), key, bytes.NewReader([]byte(body)), meta); err != nil {
		t.Fatalf("upload %q: %v", key, err)
	}
}

func TestBuildKey_NamespaceTenant(t *testing.T) {
	got := storage.BuildKey(port.StorageMeta{
		TenantID: "pemkot-surabaya",
		Module:   "surat_masuk",
		EntityID: "abc-123",
	}, "scan.pdf")
	want := "pemkot-surabaya/surat_masuk/abc-123/scan.pdf"
	if got != want {
		t.Errorf("BuildKey: mau %q dapat %q", want, got)
	}
}

func TestBuildKey_KomponenKosongDilewati(t *testing.T) {
	got := storage.BuildKey(port.StorageMeta{TenantID: "t1"}, "f.txt")
	if got != "t1/f.txt" {
		t.Errorf("BuildKey: mau %q dapat %q", "t1/f.txt", got)
	}
}

// TestLocal_UploadDownload_KontenIdentik adalah DoD PRD: upload lalu download
// menghasilkan konten identik (via port, driver local).
func TestLocal_UploadDownload_KontenIdentik(t *testing.T) {
	s := newLocal(t)
	key := storage.BuildKey(port.StorageMeta{TenantID: "t1", Module: "surat_masuk", EntityID: "1"}, "a.txt")
	mustUpload(t, s, key, "halo dunia", port.StorageMeta{ContentType: "text/plain"})

	rc, err := s.Download(context.Background(), key)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "halo dunia" {
		t.Errorf("konten: mau %q dapat %q", "halo dunia", string(got))
	}
}

// TestLocal_IsolasiKeyPerTenant membuktikan key dua tenant tidak bertabrakan dan
// List berprefix tenant hanya mengembalikan objek tenant tersebut.
func TestLocal_IsolasiKeyPerTenant(t *testing.T) {
	s := newLocal(t)
	keyA := storage.BuildKey(port.StorageMeta{TenantID: "tenant-a", Module: "m", EntityID: "1"}, "f.txt")
	keyB := storage.BuildKey(port.StorageMeta{TenantID: "tenant-b", Module: "m", EntityID: "1"}, "f.txt")
	mustUpload(t, s, keyA, "punya A", port.StorageMeta{})
	mustUpload(t, s, keyB, "punya B", port.StorageMeta{})

	keys, err := s.List(context.Background(), "tenant-a/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(keys) != 1 || keys[0] != keyA {
		t.Fatalf("list tenant-a: mau [%q] dapat %v", keyA, keys)
	}
}

func TestLocal_DownloadNotFound(t *testing.T) {
	s := newLocal(t)
	_, err := s.Download(context.Background(), "tidak/ada.txt")
	if !errors.Is(err, port.ErrObjectNotFound) {
		t.Errorf("mau ErrObjectNotFound, dapat %v", err)
	}
}

func TestLocal_DeleteIdempoten(t *testing.T) {
	s := newLocal(t)
	key := "t1/m/1/f.txt"
	mustUpload(t, s, key, "x", port.StorageMeta{})
	if err := s.Delete(context.Background(), key); err != nil {
		t.Fatalf("delete pertama: %v", err)
	}
	// Hapus ulang key yang sudah tiada tetap sukses (idempoten).
	if err := s.Delete(context.Background(), key); err != nil {
		t.Errorf("delete kedua harus idempoten, dapat %v", err)
	}
	if _, err := s.Download(context.Background(), key); !errors.Is(err, port.ErrObjectNotFound) {
		t.Errorf("setelah delete harus not-found, dapat %v", err)
	}
}

// TestLocal_TolakPathTraversal memastikan key dengan ".." tidak bisa menulis di
// luar root storage.
func TestLocal_TolakPathTraversal(t *testing.T) {
	s := newLocal(t)
	err := s.Upload(context.Background(), "../../etc/evil", bytes.NewReader([]byte("x")), port.StorageMeta{})
	if err == nil {
		t.Fatal("upload path traversal seharusnya ditolak")
	}
}

func TestNewFromConfig_DriverTakDikenal(t *testing.T) {
	if _, err := storage.NewFromConfig(config.StorageConfig{Driver: "gdrive"}); err == nil {
		t.Fatal("driver tak dikenal seharusnya error")
	}
}
