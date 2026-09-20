-- +goose Up
CREATE UNIQUE INDEX ux_notification_templates_key_no_channel
ON notification_templates (key)
WHERE channel IS NULL;

CREATE UNIQUE INDEX ux_notification_templates_key_channel
ON notification_templates (key, channel)
WHERE channel IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS ux_notification_templates_key_channel;
DROP INDEX IF EXISTS ux_notification_templates_key_no_channel;
