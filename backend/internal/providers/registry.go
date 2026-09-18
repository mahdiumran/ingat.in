package providers

import (
	"fmt"
	"sort"
)

// Registry memetakan Kind ke konstruktor provider.
//
// Dengan pola ini, menambah penyedia baru cukup mendaftarkan konstruktornya
// di sini — tidak ada perubahan pada outbox, API, atau UI.
type constructor func(cfg Config) Provider

var registry = map[string]constructor{
	KindTelegramBot: func(cfg Config) Provider { return NewTelegram(cfg) },
	KindWAHA:        func(cfg Config) Provider { return NewWAHA(cfg) },
	KindFonnte:      func(cfg Config) Provider { return NewFonnte(cfg) },
	KindWablas:      func(cfg Config) Provider { return NewWablas(cfg) },
	KindStarsender:  func(cfg Config) Provider { return NewStarsender(cfg) },
	KindCustomHTTP:  func(cfg Config) Provider { return NewCustomHTTP(cfg) },
}

// New membuat provider berdasarkan cfg.Kind.
func New(cfg Config) (Provider, error) {
	ctor, ok := registry[cfg.Kind]
	if !ok {
		return nil, fmt.Errorf("%w: jenis provider %q tidak dikenal", ErrUnsupported, cfg.Kind)
	}
	return ctor(cfg), nil
}

// IsKnownKind melaporkan apakah sebuah Kind terdaftar.
func IsKnownKind(kind string) bool {
	_, ok := registry[kind]
	return ok
}

// SupportedKinds mengembalikan daftar Kind yang terdaftar (terurut).
func SupportedKinds() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// KindsForChannel mengembalikan Kind yang masuk akal untuk sebuah kanal.
//
// Dipakai panel untuk membatasi pilihan saat membuat provider baru.
func KindsForChannel(channel string) []string {
	switch channel {
	case "whatsapp":
		return []string{KindWAHA, KindFonnte, KindWablas, KindStarsender, KindCustomHTTP}
	case "telegram":
		return []string{KindTelegramBot}
	default:
		return SupportedKinds()
	}
}
