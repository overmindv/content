-- +goose Up
SET LOCAL lock_timeout = '5s';
ALTER TABLE outbox
    ADD COLUMN IF NOT EXISTS claim_token UUID,
    ADD COLUMN IF NOT EXISTS lease_until TIMESTAMPTZ;

-- +goose Down
-- Keep the lease columns so a later upgrade can recover unfinished events.
SELECT 1;
