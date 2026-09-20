-- +goose Up
UPDATE notification_templates
SET body_tpl = body_tpl || E'\n\nDeskripsi : {{.Description}}',
    updated_at = now()
WHERE key IN ('TODO_CREATED','REMINDER_OFFSET','RFS_UPCOMING')
  AND channel IN ('telegram','whatsapp')
  AND body_tpl NOT LIKE '%Deskripsi :%';

-- +goose Down
UPDATE notification_templates
SET body_tpl = replace(body_tpl, E'\n\nDeskripsi : {{.Description}}', ''),
    updated_at = now()
WHERE key IN ('TODO_CREATED','REMINDER_OFFSET','RFS_UPCOMING')
  AND channel IN ('telegram','whatsapp');
