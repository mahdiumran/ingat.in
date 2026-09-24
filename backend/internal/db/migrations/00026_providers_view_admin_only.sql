-- +goose Up

-- ============================================================================
-- F33 — Batasi `providers.view` (Notification Center) hanya untuk admin.
--
-- Revisi kebijakan F32: awalnya role `noc` dan `adminnoc` ikut diberi izin
-- `providers.view`, tetapi kebijakan yang diinginkan adalah HANYA admin (super
-- user) yang dapat melihat Notification Center (Providers, Notification
-- Targets, Notification Templates) secara default.
--
-- Escalation Policies TIDAK dibatasi (tetap dapat dilihat semua role).
-- Operator dapat memberi izin ini ke role lain kapan saja melalui dialog
-- Role Permissions; migrasi ini hanya menetapkan default.
--
-- Idempoten: hanya mengubah baris role yang bukan `admin`. Baris `admin`
-- dipertahankan TRUE (walaupun super user selalu lolos pemeriksaan).
-- ============================================================================

UPDATE role_permissions
   SET allowed = FALSE, updated_by = 'seed', updated_at = now()
 WHERE action = 'providers.view'
   AND role <> 'admin'
   AND allowed = TRUE;

-- Pastikan baris admin tetap ada dan bernilai TRUE.
INSERT INTO role_permissions (role, action, allowed, updated_by) VALUES
    ('admin', 'providers.view', TRUE, 'seed')
ON CONFLICT (role, action) DO UPDATE
    SET allowed = TRUE, updated_by = 'seed', updated_at = now();

-- +goose Down

-- Kembalikan kebijakan F32 (noc & adminnoc = TRUE).
UPDATE role_permissions
   SET allowed = TRUE, updated_by = 'seed', updated_at = now()
 WHERE action = 'providers.view'
   AND role IN ('adminnoc', 'noc');
