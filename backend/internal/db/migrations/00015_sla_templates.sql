-- +goose Up

-- ============================================================================
-- F20 — Perkaya template notifikasi SLA.
--
-- Mesin SLA kini aktif mengirim SLA_WARNING/SLA_BREACH (target per prioritas,
-- kalender 24 jam). Template lama hanya memuat Status/Owner/Team; versi baru
-- menambahkan Prioritas, waktu sisa/terlewat, dan konteks yang dapat
-- ditindaklanjuti.
-- ============================================================================

UPDATE notification_templates
SET body_tpl = E'🚨 SLA TERLAMPAUI {{.RefNo}}\n{{.Title}}\n\nPrioritas : {{.Priority}}\nOwner     : {{dash .Owner}}\nTerlewat  : {{dash .Remaining}}\nWaktu     : {{.CreatedAt}}',
    updated_at = now()
WHERE key = 'SLA_BREACH' AND channel = 'telegram';

UPDATE notification_templates
SET body_tpl = E'*SLA TERLAMPAUI* {{.RefNo}}\n{{.Title}}\n\nPrioritas: {{.Priority}}\nOwner: {{dash .Owner}}\nTerlewat: {{dash .Remaining}}',
    updated_at = now()
WHERE key = 'SLA_BREACH' AND channel = 'whatsapp';

UPDATE notification_templates
SET body_tpl = E'⚠️ PERINGATAN SLA {{.RefNo}}\n{{.Title}}\n\nPrioritas : {{.Priority}}\nOwner     : {{dash .Owner}}\nSisa      : {{dash .Remaining}}\nWaktu     : {{.CreatedAt}}',
    updated_at = now()
WHERE key = 'SLA_WARNING' AND channel = 'telegram';

UPDATE notification_templates
SET body_tpl = E'*PERINGATAN SLA* {{.RefNo}}\n{{.Title}}\n\nPrioritas: {{.Priority}}\nOwner: {{dash .Owner}}\nSisa: {{dash .Remaining}}',
    updated_at = now()
WHERE key = 'SLA_WARNING' AND channel = 'whatsapp';

-- +goose Down

UPDATE notification_templates
SET body_tpl = E'🚨 SLA TERLAMPAUI\n{{.RefNo}} — {{.Title}}\n\nStatus : {{.Status}}\nOwner  : {{.Owner}}\nTeam   : {{.Team}}',
    updated_at = now()
WHERE key = 'SLA_BREACH' AND channel = 'telegram';

UPDATE notification_templates
SET body_tpl = E'*SLA TERLAMPAUI*\n{{.RefNo}} — {{.Title}}\nStatus: {{.Status}}\nOwner: {{.Owner}}',
    updated_at = now()
WHERE key = 'SLA_BREACH' AND channel = 'whatsapp';

UPDATE notification_templates
SET body_tpl = E'⚠️ SLA MENDEKATI BATAS\n{{.RefNo}} — {{.Title}}\n\nStatus : {{.Status}}\nOwner  : {{.Owner}}\nTeam   : {{.Team}}',
    updated_at = now()
WHERE key = 'SLA_WARNING' AND channel = 'telegram';

UPDATE notification_templates
SET body_tpl = E'*SLA MENDEKATI BATAS*\n{{.RefNo}} — {{.Title}}\nStatus: {{.Status}}\nOwner: {{.Owner}}',
    updated_at = now()
WHERE key = 'SLA_WARNING' AND channel = 'whatsapp';
