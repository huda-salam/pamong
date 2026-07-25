// Package storage adalah driven adapter yang menyediakan port.StoragePort.
// storage.go adalah entry: factory pemilih driver (minio|s3|local) dan helper
// penyusun key ber-namespace tenant. Implementasi konkret ada di sub-package drivers.
package storage

import (
	"fmt"
	"path"
	"strings"

	"github.com/huda-salam/pamong/core/config"
	"github.com/huda-salam/pamong/infra/storage/drivers"
	"github.com/huda-salam/pamong/port"
)

// NewFromConfig membuat StoragePort siap pakai dari konfigurasi. Driver dipilih
// berdasarkan cfg.Driver; pemanggil tetap bergantung pada port.StoragePort sehingga
// mengganti driver (local↔minio) tidak mengubah kode pemanggil (PRD AC).
//
// Menambah driver baru: tambahkan case di sini dan implementasikan port.StoragePort
// di infra/storage/drivers (titik ekstensi #1, registry pattern).
func NewFromConfig(cfg config.StorageConfig) (port.StoragePort, error) {
	switch cfg.Driver {
	case "minio", "s3":
		return drivers.NewMinIO(drivers.MinIOConfig{
			Endpoint:  cfg.Endpoint,
			Bucket:    cfg.Bucket,
			AccessKey: cfg.AccessKey,
			SecretKey: cfg.SecretKey,
		})
	case "local", "":
		return drivers.NewLocal(drivers.LocalConfig{
			Root:   cfg.Endpoint, // untuk driver local, endpoint = direktori root
			Bucket: cfg.Bucket,
		})
	default:
		return nil, fmt.Errorf("storage: driver tidak dikenal: %q (pilihan: minio|s3|local)", cfg.Driver)
	}
}

// BuildKey menyusun object key ber-namespace per tenant sesuai konvensi
// {tenant}/{module}/{entity}/{filename}. Prefix tenant inilah yang menegakkan
// isolasi antar tenant di storage; pemanggil (use case) mengisi meta dari
// gateway.Context. Komponen kosong dilewati agar key tetap rapi.
func BuildKey(meta port.StorageMeta, filename string) string {
	parts := make([]string, 0, 4)
	for _, p := range []string{meta.TenantID, meta.Module, meta.EntityID, filename} {
		if p = strings.Trim(p, "/"); p != "" {
			parts = append(parts, p)
		}
	}
	return path.Join(parts...)
}
