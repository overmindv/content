-- +goose Up
CREATE TABLE content_catalog_clock (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    watermark BIGINT NOT NULL CHECK (watermark >= 0)
);
INSERT INTO content_catalog_clock (watermark) VALUES (0);

CREATE TABLE content_catalog_materials (
    material_id UUID PRIMARY KEY,
    first_version BIGINT NOT NULL CHECK (first_version >= 0)
);

CREATE TABLE content_catalog_versions (
    material_id UUID NOT NULL REFERENCES content_catalog_materials (material_id),
    version BIGINT NOT NULL CHECK (version >= 0),
    revision_id UUID NOT NULL,
    tombstone BOOLEAN NOT NULL,
    metadata JSONB,
    PRIMARY KEY (material_id, version),
    CHECK (tombstone = (metadata IS NULL))
);

INSERT INTO content_catalog_materials (material_id, first_version)
SELECT i.id, 0 FROM content_items AS i
JOIN content_revisions AS r ON r.id = i.published_revision_id AND r.content_item_id = i.id
WHERE i.status IN ('published', 'archived');

INSERT INTO content_catalog_versions (material_id, version, revision_id, tombstone, metadata)
SELECT i.id, 0, i.published_revision_id, i.status = 'archived',
    CASE WHEN i.status = 'published' THEN jsonb_build_object(
        'title', left(i.title, 256), 'type', i.type, 'format', r.format, 'estimated_minutes', i.estimated_minutes,
        'topic_ids', ARRAY(SELECT t.topic_id FROM content_item_topics AS t WHERE t.content_item_id = i.id ORDER BY t.topic_id)
    ) END
FROM content_items AS i JOIN content_catalog_materials AS m ON m.material_id = i.id
JOIN content_revisions AS r ON r.id = i.published_revision_id AND r.content_item_id = i.id;

CREATE TABLE content_read_confirmations (
    material_id UUID NOT NULL REFERENCES content_items (id) ON DELETE CASCADE,
    revision_id UUID NOT NULL,
    user_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    PRIMARY KEY (material_id, revision_id, user_id)
);

-- +goose Down
ALTER TABLE content_read_confirmations ADD CONSTRAINT no_read_history CHECK (FALSE);
ALTER TABLE content_catalog_versions ADD CONSTRAINT no_catalog_history CHECK (FALSE);
DROP TABLE content_read_confirmations;
DROP TABLE content_catalog_versions;
DROP TABLE content_catalog_materials;
DROP TABLE content_catalog_clock;
