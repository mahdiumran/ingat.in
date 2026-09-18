// Package repository adalah lapisan akses data. Satu file per entitas.
//
// Konvensi (diadopsi dari mcnvpn/internal/repository):
//   - Store membungkus *pgxpool.Pool
//   - SQL mentah dengan placeholder posisional $1..$n (tanpa ORM)
//   - baris hilang dipetakan ke ErrNotFound
//   - helper scanXxx(rows pgx.Rows) untuk hasil banyak baris
package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound dikembalikan bila baris yang diminta tidak ada.
var ErrNotFound = errors.New("data tidak ditemukan")

// ErrConflict dikembalikan bila ada pelanggaran keunikan.
var ErrConflict = errors.New("data sudah ada")

// Store adalah pintu masuk seluruh operasi database.
type Store struct {
	pool *pgxpool.Pool
}

// New membuat Store baru.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Pool mengembalikan pool mentah untuk query ad-hoc (dipakai worker).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Tx menjalankan fn di dalam satu transaksi.
func (s *Store) Tx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Now mengembalikan waktu UTC. Semua timestamp disimpan UTC.
func (s *Store) Now() time.Time { return time.Now().UTC() }

// Ping memverifikasi koneksi database.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// mapErr memetakan error pgx ke sentinel error paket ini.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
