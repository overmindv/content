-- +goose Up
SET LOCAL lock_timeout = '5s';

ALTER TABLE content_catalog_versions
    ADD COLUMN IF NOT EXISTS activity_occurred_at TIMESTAMPTZ;

-- +goose Down
-- Keep event times when rolling back the binary.
SELECT 1;
