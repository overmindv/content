-- +goose Up
ALTER TABLE content_items
    ADD COLUMN moderation_hidden BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN moderation_version BIGINT NOT NULL DEFAULT 0 CHECK (moderation_version >= 0);

CREATE TABLE content_moderation_audit (
    material_id UUID NOT NULL,
    version BIGINT NOT NULL CHECK (version > 0),
    published_revision_id UUID NOT NULL,
    actor_id UUID NOT NULL,
    hidden BOOLEAN NOT NULL,
    reason TEXT NOT NULL CHECK (octet_length(reason) BETWEEN 1 AND 1024),
    created_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    PRIMARY KEY (material_id, version)
);

-- +goose Down
ALTER TABLE content_moderation_audit ADD CONSTRAINT no_moderation_history CHECK (FALSE);
DROP TABLE content_moderation_audit;
ALTER TABLE content_items DROP COLUMN moderation_version, DROP COLUMN moderation_hidden;
