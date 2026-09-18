package repository

import (
	"context"
	"sync/atomic"
)

// schemaVersionCache menyimpan versi skema yang sudah terbaca.
// Versi skema tidak berubah selama proses berjalan (migrasi hanya dijalankan
// oleh mode `migrate` sebelum `api` start), sehingga aman di-cache.
var schemaVersionCache atomic.Int64

// schemaVersionKnown menandai apakah cache sudah pernah terisi.
var schemaVersionKnown atomic.Bool

// SetSchemaVersion mengisi cache versi skema.
func SetSchemaVersion(v int64) {
	schemaVersionCache.Store(v)
	schemaVersionKnown.Store(true)
}

// SchemaVersionSafe mengembalikan versi skema dari cache tanpa menyentuh
// database. Versi diisi saat bootstrap oleh mode `migrate` atau saat API start.
func (s *Store) SchemaVersionSafe(_ context.Context) (int64, error) {
	if schemaVersionKnown.Load() {
		return schemaVersionCache.Load(), nil
	}
	return 0, nil
}
