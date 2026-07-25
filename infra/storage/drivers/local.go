// Package drivers berisi implementasi port.StoragePort: local (filesystem, untuk
// dev/test) dan minio (S3-compat, produksi). Driver hanya menyimpan objek per key;
// namespacing tenant dibangun pemanggil lewat storage.BuildKey, dan otorisasi
// lampiran dicek di use case/gateway — bukan di sini.
package drivers

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/huda-salam/pamong/port"
)

// Local menyimpan objek sebagai file di bawah base (root/bucket). Key menjadi path
// relatif. Untuk dev/test saja (PRD storage F2): tak ada replikasi, versioning,
// atau kontrol akses object-level seperti backend S3.
type Local struct {
	base string // root/bucket, absolut
}

// LocalConfig mengonfigurasi driver local.
type LocalConfig struct {
	Root   string // direktori root; kosong = ./data/storage
	Bucket string // sub-direktori di bawah Root (analog bucket S3)
}

var _ port.StoragePort = (*Local)(nil)

// NewLocal membuat driver filesystem dan memastikan direktori base ada.
func NewLocal(cfg LocalConfig) (*Local, error) {
	root := cfg.Root
	if root == "" {
		root = filepath.Join("data", "storage")
	}
	base := root
	if cfg.Bucket != "" {
		base = filepath.Join(root, cfg.Bucket)
	}
	abs, err := filepath.Abs(base)
	if err != nil {
		return nil, fmt.Errorf("storage/local: resolve path %q: %w", base, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("storage/local: buat direktori %q: %w", abs, err)
	}
	return &Local{base: abs}, nil
}

// resolve memetakan key ke path absolut di dalam base, menolak path traversal.
// Key yang absolut atau memuat komponen ".." yang keluar dari base ditolak tegas
// (bukan diam-diam dinormalisasi), sehingga upaya escape gagal, bukan salah simpan.
func (l *Local) resolve(key string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(key))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("storage/local: key %q keluar dari root storage", key)
	}
	return filepath.Join(l.base, clean), nil
}

// Upload menulis stream r ke file pada key. Direktori antara dibuat otomatis.
// Streaming via io.Copy — tidak memuat seluruh isi ke memori (PRD NFR).
func (l *Local) Upload(ctx context.Context, key string, r io.Reader, meta port.StorageMeta) error {
	_ = meta // metadata (content-type dsb) tak dipersist di driver local dev/test
	full, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return fmt.Errorf("storage/local: buat direktori untuk %q: %w", key, err)
	}
	f, err := os.Create(full)
	if err != nil {
		return fmt.Errorf("storage/local: buat file %q: %w", key, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("storage/local: tulis %q: %w", key, err)
	}
	return nil
}

// Download membuka file pada key untuk dibaca. Objek tak ada → port.ErrObjectNotFound.
func (l *Local) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	full, err := l.resolve(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("storage/local: %q: %w", key, port.ErrObjectNotFound)
		}
		return nil, fmt.Errorf("storage/local: buka %q: %w", key, err)
	}
	return f, nil
}

// Delete menghapus objek pada key. Idempoten: key yang tidak ada bukan error
// (selaras semantik hapus S3).
func (l *Local) Delete(ctx context.Context, key string) error {
	full, err := l.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage/local: hapus %q: %w", key, err)
	}
	return nil
}

// List mengembalikan key (slash-separated, relatif base) yang berawalan prefix,
// terurut. Dipakai mis. menghitung lampiran satu entity via prefix tenant/module/id.
func (l *Local) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	err := filepath.WalkDir(l.base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(l.base, p)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("storage/local: list prefix %q: %w", prefix, err)
	}
	sort.Strings(keys)
	return keys, nil
}
