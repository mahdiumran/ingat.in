package repository

import (
	"context"
	"encoding/json"
	"fmt"
)

/* ---------------------------------------------------------------------------
   Settings — konfigurasi runtime & preferensi per pengguna.
   --------------------------------------------------------------------------- */

// GetSetting mengambil nilai setting sebagai map. ErrNotFound bila tidak ada.
func (s *Store) GetSetting(ctx context.Context, key string) (map[string]any, error) {
	var raw []byte
	err := s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key=$1`, key).Scan(&raw)
	if err != nil {
		return nil, mapErr(err)
	}
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out, nil
}

// SetSetting menyimpan nilai setting (upsert).
func (s *Store) SetSetting(ctx context.Context, key string, value map[string]any, by string) error {
	if value == nil {
		value = map[string]any{}
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_by, updated_at)
		VALUES ($1,$2,$3, now())
		ON CONFLICT (key) DO UPDATE
			SET value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		key, value, by)
	return err
}

// userSettingKey menyusun kunci setting per-pengguna.
//
// Contoh: user:admin:notifications_last_seen_id.
func userSettingKey(username, name string) string {
	return fmt.Sprintf("user:%s:%s", username, name)
}

// GetUserSettingInt mengambil setting integer milik seorang pengguna.
// Nilai 0 dikembalikan bila belum pernah diset (aman untuk posisi baca).
func (s *Store) GetUserSettingInt(ctx context.Context, username, name string) (int64, error) {
	v, err := s.GetSetting(ctx, userSettingKey(username, name))
	if err != nil {
		return 0, err
	}
	switch n := v["value"].(type) {
	case float64:
		return int64(n), nil
	case int64:
		return n, nil
	case json.Number:
		i, _ := n.Int64()
		return i, nil
	default:
		return 0, nil
	}
}

// SetUserSettingInt menyimpan setting integer milik seorang pengguna.
func (s *Store) SetUserSettingInt(ctx context.Context, username, name string, value int64) error {
	return s.SetSetting(ctx, userSettingKey(username, name), map[string]any{"value": value}, username)
}

// GetUserSettingsMap mengambil seluruh setting milik pengguna sebagai map datar
// (nama -> nilai), memudahkan endpoint preferensi.
func (s *Store) GetUserSettingsMap(ctx context.Context, username string) (map[string]any, error) {
	prefix := userSettingKey(username, "")
	rows, err := s.pool.Query(ctx,
		`SELECT key, value FROM settings WHERE key LIKE $1`, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]any{}
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		name := key[len(prefix):]
		var wrapper struct {
			Value any `json:"value"`
		}
		_ = json.Unmarshal(raw, &wrapper)
		out[name] = wrapper.Value
	}
	return out, rows.Err()
}
