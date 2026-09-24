-- +goose Up

-- ============================================================================
-- F30 — Format notifikasi PEMBUATAN task / daily task / RFS-EWO.
--
-- Standar baru untuk pesan "dibuat" (event created), berlaku untuk telegram &
-- whatsapp:
--   - Tanggal Dibuat : tanggal + jam item dibuat
--   - Tenggat Waktu  : tanggal + jam tenggat (due/expire), jika diset
--                      (daily task default 23:59 WIB tetap otomatis)
--   - Dibuat Oleh    : username pembuat
--
-- Perubahan:
--   1. TODO_CREATED       — tambah "Tanggal Dibuat" + "Dibuat Oleh", perjelas
--                           label "Tenggat Waktu".
--   2. DAILY_TASK_CREATED  — idem.
--   3. RFS_CREATED (BARU)  — template khusus pembuatan RFS/EWO. Sebelumnya
--                           pembuatan RFS memakai RFS_UPCOMING (template
--                           pengingat) sehingga label offset kosong.
--
-- Template pengingat (RFS_UPCOMING/RFS_TODAY/RFS_LATE) TIDAK diubah.
--
-- Nilai waktu memakai helper yang sudah ada: {{.CreatedAt}} = FormatWIB
-- (tanggal + jam), {{dash .DueAt}} / {{dash .ExpireAt}} = tenggat.
-- ============================================================================

-- ---------------------------------------------------------------------------
-- 1) TODO_CREATED — update body untuk kanal telegram & whatsapp.
-- ---------------------------------------------------------------------------
UPDATE notification_templates SET
    body_tpl = E'✅ TODO BARU {{.RefNo}}\n{{.Title}}\n\nPrioritas       : {{.Priority}}\nOwner           : {{.Owner}}\nTanggal Dibuat  : {{.CreatedAt}}\nTenggat Waktu   : {{dash .DueAt}}\nDibuat Oleh     : {{dash .CreatedBy}}\n\nDeskripsi : {{or .Description "—"}}',
    updated_by = 'seed',
    updated_at = now()
WHERE key = 'TODO_CREATED' AND channel = 'telegram';

UPDATE notification_templates SET
    body_tpl = E'*TODO BARU* {{.RefNo}}\n{{.Title}}\n\nPrioritas       : {{.Priority}}\nOwner           : {{.Owner}}\nTanggal Dibuat  : {{.CreatedAt}}\nTenggat Waktu   : {{dash .DueAt}}\nDibuat Oleh     : {{dash .CreatedBy}}\n\nDeskripsi : {{or .Description "—"}}',
    updated_by = 'seed',
    updated_at = now()
WHERE key = 'TODO_CREATED' AND channel = 'whatsapp';

-- ---------------------------------------------------------------------------
-- 2) DAILY_TASK_CREATED — update body untuk kanal telegram & whatsapp.
-- ---------------------------------------------------------------------------
UPDATE notification_templates SET
    body_tpl = E'📋 DAILY TASK BARU {{.RefNo}}\n{{.Title}}\n\nOwner           : {{.Owner}}\nTanggal Dibuat  : {{.CreatedAt}}\nTenggat Waktu   : {{dash .DueAt}}\nDibuat Oleh     : {{dash .CreatedBy}}\n\nDeskripsi : {{or .Description "—"}}',
    updated_by = 'seed',
    updated_at = now()
WHERE key = 'DAILY_TASK_CREATED' AND channel = 'telegram';

UPDATE notification_templates SET
    body_tpl = E'*DAILY TASK BARU* {{.RefNo}}\n{{.Title}}\n\nOwner           : {{.Owner}}\nTanggal Dibuat  : {{.CreatedAt}}\nTenggat Waktu   : {{dash .DueAt}}\nDibuat Oleh     : {{dash .CreatedBy}}\n\nDeskripsi : {{or .Description "—"}}',
    updated_by = 'seed',
    updated_at = now()
WHERE key = 'DAILY_TASK_CREATED' AND channel = 'whatsapp';

-- ---------------------------------------------------------------------------
-- 3) RFS_CREATED (BARU) — pembuatan RFS/EWO.
-- ---------------------------------------------------------------------------
INSERT INTO notification_templates (key, item_type, channel, subject_tpl, body_tpl, severity, description) VALUES
(
  'RFS_CREATED', 'rfs', 'telegram',
  '',
  E'📅 RFS / EWO BARU {{.RefNo}}\n{{.Title}}\n\nCustomer        : {{dash .CustomerName}}\nPaket           : {{.ServicePackage}} / {{.Bandwidth}}\nSite            : {{dash .Site}}\nPIC NOC         : {{dash .PicNOC}}\nPIC Sales       : {{dash .PicSales}}\nTanggal Dibuat  : {{.CreatedAt}}\nTenggat Waktu   : {{dash .ExpireAt}}\nDibuat Oleh     : {{dash .CreatedBy}}\n\nDeskripsi : {{or .Description "—"}}',
  'info', 'Notifikasi saat RFS/Aktivasi (EWO) dibuat.'
),
(
  'RFS_CREATED', 'rfs', 'whatsapp',
  '',
  E'*RFS / EWO BARU* {{.RefNo}}\n{{.Title}}\n\nCustomer        : {{dash .CustomerName}}\nPaket           : {{.ServicePackage}} / {{.Bandwidth}}\nTanggal Dibuat  : {{.CreatedAt}}\nTenggat Waktu   : {{dash .ExpireAt}}\nDibuat Oleh     : {{dash .CreatedBy}}\n\nDeskripsi : {{or .Description "—"}}',
  'info', 'Notifikasi saat RFS/Aktivasi (EWO) dibuat (WhatsApp).'
)
ON CONFLICT (key, channel) DO UPDATE SET
    item_type = EXCLUDED.item_type,
    subject_tpl = EXCLUDED.subject_tpl,
    body_tpl = EXCLUDED.body_tpl,
    severity = EXCLUDED.severity,
    description = EXCLUDED.description,
    updated_by = 'seed',
    updated_at = now();

-- +goose Down

DELETE FROM notification_templates WHERE key = 'RFS_CREATED';
