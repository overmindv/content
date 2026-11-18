-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY idx_content_items_unhidden_order
    ON content_items (created_at DESC, id DESC) WHERE NOT moderation_hidden;

-- +goose Down
DROP INDEX CONCURRENTLY idx_content_items_unhidden_order;
