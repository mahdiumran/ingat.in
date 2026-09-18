// Package notify berisi logika notifikasi: resolusi provider, rendering
// template, antrean outbox, fan-out offset, dan eskalasi.
package notify

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"ingatin/backend/internal/config"
	"ingatin/backend/internal/crypto"
	"ingatin/backend/internal/models"
	"ingatin/backend/internal/providers"
	"ingatin/backend/internal/repository"
)

// ErrNoProvider dikembalikan bila kanal tidak memiliki provider aktif.
var ErrNoProvider = errors.New("tidak ada provider aktif untuk kanal ini")

// Resolver membangun provider konkret dari konfigurasi di database.
//
// Tanggung jawabnya memisahkan dua hal yang berbeda:
//   - data (repository) menyimpan ciphertext + pengaturan,
//   - providers (pkg) hanya peduli cara mengirim.
type Resolver struct {
	cfg    *config.Config
	store  *repository.Store
	outbox *Outbox
}

// NewResolver membuat Resolver.
func NewResolver(cfg *config.Config, store *repository.Store) *Resolver {
	return &Resolver{cfg: cfg, store: store}
}

// BuildFromRecord membangun provider dari record database.
//
// Nilai api_key_enc didekripsi memakai INGATIN_CREDENTIAL_KEY. Bila dekripsi
// gagal (mis. kunci berubah), error dilaporkan agar terlihat di panel/audit
// alih-alih diam-diam mengirim tanpa kredensial.
func (r *Resolver) BuildFromRecord(p *models.NotificationProvider, apiKeyEnc string) (providers.Provider, error) {
	apiKey := ""
	if apiKeyEnc != "" {
		plain, err := crypto.Decrypt(r.cfg.CredentialKey, apiKeyEnc)
		if err != nil {
			return nil, fmt.Errorf("dekripsi API key provider %s: %w", p.Label, err)
		}
		apiKey = plain
	}

	extra := map[string]any{}
	for k, v := range p.Extra {
		extra[k] = v
	}

	return providers.New(providers.Config{
		Kind:    p.Kind,
		Label:   p.Label,
		BaseURL: p.BaseURL,
		APIKey:  apiKey,
		Extra:   extra,
	})
}

// ProviderForChannel mengembalikan provider aktif untuk sebuah kanal,
// memakai override ProviderID bila diberikan.
func (r *Resolver) ProviderForChannel(ctx context.Context, channel string, override *uuid.UUID) (providers.Provider, *models.NotificationProvider, error) {
	var (
		rec *models.NotificationProvider
		enc string
		err error
	)

	if override != nil {
		rec, enc, err = r.store.ProviderExtraWithSecret(ctx, *override)
		if err != nil {
			return nil, nil, fmt.Errorf("provider yang dipilih tidak ditemukan: %w", err)
		}
		if !rec.IsActive {
			return nil, nil, fmt.Errorf("provider %q tidak aktif", rec.Label)
		}
	} else {
		var def *models.NotificationProvider
		def, err = r.store.GetDefaultProviderForChannel(ctx, channel)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return nil, nil, fmt.Errorf("%w: %s", ErrNoProvider, channel)
			}
			return nil, nil, err
		}
		var e error
		rec, enc, e = r.store.ProviderExtraWithSecret(ctx, def.ID)
		if e != nil {
			return nil, nil, e
		}
	}

	provider, err := r.BuildFromRecord(rec, enc)
	if err != nil {
		return nil, rec, err
	}
	return provider, rec, nil
}

// Outbox mengembalikan outbox yang terhubung ke resolver ini.
//
// API memakai ini untuk mengantrikan notifikasi peristiwa (mis. item dibuat),
// sehingga handler tidak perlu menyusun Outbox sendiri.
func (r *Resolver) Outbox() *Outbox {
	if r.outbox == nil {
		r.outbox = NewOutbox(r.cfg, r.store, r)
	}
	return r.outbox
}

// EncryptKey membantu panel menyimpan API key dalam bentuk terenkripsi.
func (r *Resolver) EncryptKey(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	return crypto.Encrypt(r.cfg.CredentialKey, plain)
}
