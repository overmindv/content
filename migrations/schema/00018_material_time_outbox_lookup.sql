-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY outbox_material_time_lookup_idx ON outbox (topic, message_key, id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS outbox_material_time_lookup_idx;
