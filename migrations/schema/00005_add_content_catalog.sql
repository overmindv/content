-- +goose Up
ALTER TABLE content_items
    ADD COLUMN estimated_minutes INTEGER NOT NULL DEFAULT 0 CHECK (estimated_minutes BETWEEN 0 AND 480),
    ADD COLUMN learning_outcomes TEXT[] NOT NULL DEFAULT '{}' CHECK (cardinality(learning_outcomes) <= 8);

CREATE TABLE content_item_topics (
    content_item_id UUID NOT NULL REFERENCES content_items (id) ON DELETE CASCADE,
    topic_id UUID NOT NULL,
    PRIMARY KEY (content_item_id, topic_id)
);

CREATE INDEX idx_content_item_topics_topic ON content_item_topics (topic_id, content_item_id);
CREATE INDEX idx_content_items_catalog_order ON content_items (created_at DESC, id DESC);
CREATE INDEX idx_content_items_catalog_author ON content_items (created_by, created_at DESC, id DESC);

-- +goose Down
DROP INDEX idx_content_items_catalog_author;
DROP INDEX idx_content_items_catalog_order;
DROP TABLE content_item_topics;
ALTER TABLE content_items DROP COLUMN learning_outcomes, DROP COLUMN estimated_minutes;
