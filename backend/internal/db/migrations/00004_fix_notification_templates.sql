-- +goose Up
UPDATE notification_templates
SET body_tpl = replace(body_tpl, '{{.PicNoc}}', '{{.PicNOC}}'),
    updated_at = now()
WHERE key IN ('RFS_UPCOMING', 'RFS_TODAY', 'RFS_LATE')
  AND channel IN ('telegram', 'whatsapp')
  AND body_tpl LIKE '%{{.PicNoc}}%';

-- +goose Down
UPDATE notification_templates
SET body_tpl = replace(body_tpl, '{{.PicNOC}}', '{{.PicNoc}}'),
    updated_at = now()
WHERE key IN ('RFS_UPCOMING', 'RFS_TODAY', 'RFS_LATE')
  AND channel IN ('telegram', 'whatsapp')
  AND body_tpl LIKE '%{{.PicNOC}}%';
