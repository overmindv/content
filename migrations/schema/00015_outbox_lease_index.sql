-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY outbox_lease_idx ON outbox (lease_until)
    WHERE status = 'publishing';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS outbox_lease_idx;
