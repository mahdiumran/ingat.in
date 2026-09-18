// Package db mengelola koneksi PostgreSQL dan migrasi skema.
//
// Pola diadopsi dari mcnvpn/internal/db:
//   - aplikasi memakai pgx native (pgxpool)
//   - goose memakai database/sql lewat blank import stdlib, hanya untuk migrasi
//   - migrasi di-embed ke binary (//go:embed) supaya satu binary self-contained
package db

import (
	"context"
	"embed"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registrasi driver "pgx" untuk goose (database/sql)
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Connect membuat pool koneksi dan memverifikasinya dengan Ping.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	// Pool modest: cukup untuk satu instance API + worker.
	cfg.MaxConns = 10
	cfg.MinConns = 1
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("buka pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}

// Migrate menjalankan seluruh migrasi yang belum diterapkan (goose Up).
// Idempoten: aman dijalankan berulang.
func Migrate(ctx context.Context, url string) error {
	db, err := goose.OpenDBWithDriver("pgx", url)
	if err != nil {
		return fmt.Errorf("buka db untuk migrasi: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrationsFS)
	goose.SetLogger(goose.NopLogger())

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("terapkan migrasi: %w", err)
	}

	current, err := goose.GetDBVersionContext(ctx, db)
	if err == nil {
		log.Printf("db: migrasi selesai (versi skema %d)", current)
	} else {
		log.Printf("db: migrasi selesai")
	}
	return nil
}

// SchemaVersion mengembalikan versi skema saat ini (0 bila belum ada migrasi).
func SchemaVersion(ctx context.Context, url string) (int64, error) {
	db, err := goose.OpenDBWithDriver("pgx", url)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	goose.SetBaseFS(migrationsFS)
	if err := goose.SetDialect("postgres"); err != nil {
		return 0, err
	}
	return goose.GetDBVersionContext(ctx, db)
}
