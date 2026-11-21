-- +goose Up
ALTER TABLE content_change_proposal_revisions
    ADD COLUMN attachments_snapshot BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE content_assets
    ADD COLUMN proposal_id UUID,
    ADD COLUMN proposal_revision_id UUID,
    ADD FOREIGN KEY (proposal_id, proposal_revision_id)
        REFERENCES content_change_proposal_revisions (proposal_id, id) ON DELETE CASCADE,
    ADD CHECK (
        (proposal_id IS NULL AND proposal_revision_id IS NULL)
        OR (proposal_id IS NOT NULL AND proposal_revision_id IS NOT NULL AND revision_id IS NULL)
    );
CREATE INDEX idx_content_assets_proposal_revision
    ON content_assets (proposal_id, proposal_revision_id, position, id) WHERE proposal_id IS NOT NULL;
CREATE INDEX idx_content_assets_material_revision
    ON content_assets (content_item_id, revision_id, position, id) WHERE revision_id IS NOT NULL;

CREATE TABLE content_media_bindings (
    revision_id UUID PRIMARY KEY,
    material_id UUID NOT NULL,
    proposal_id UUID,
    actor_user_id UUID NOT NULL,
    inherit_from_revision_id UUID,
    file_ids UUID[] NOT NULL CHECK (cardinality(file_ids) <= 32),
    desired_version SMALLINT NOT NULL DEFAULT 1 CHECK (desired_version IN (1, 2)),
    applied_version SMALLINT NOT NULL DEFAULT 0 CHECK (applied_version BETWEEN 0 AND desired_version),
    lease_token UUID,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT statement_timestamp(),
    CHECK ((desired_version = 1 AND cardinality(file_ids) > 0)
        OR (desired_version = 2 AND cardinality(file_ids) = 0 AND inherit_from_revision_id IS NULL))
);
CREATE INDEX idx_content_media_due ON content_media_bindings (next_attempt_at, revision_id)
    WHERE applied_version < desired_version;
CREATE INDEX idx_content_media_material ON content_media_bindings (material_id) WHERE desired_version = 1;
CREATE INDEX idx_content_media_proposal ON content_media_bindings (proposal_id)
    WHERE desired_version = 1 AND proposal_id IS NOT NULL;

INSERT INTO content_media_bindings (revision_id, material_id, actor_user_id, file_ids)
SELECT a.revision_id, i.id, i.created_by, array_agg(DISTINCT a.asset_id ORDER BY a.asset_id)
FROM content_assets AS a
JOIN content_items AS i ON i.id = a.content_item_id
JOIN content_revisions AS r ON r.id = a.revision_id AND r.content_item_id = i.id
WHERE i.created_by IS NOT NULL
GROUP BY a.revision_id, i.id
HAVING count(DISTINCT a.asset_id) <= 32;

-- +goose Down
ALTER TABLE content_media_bindings ADD CONSTRAINT no_delivered_media_bindings CHECK (applied_version = 0);
ALTER TABLE content_assets ADD CONSTRAINT no_proposal_assets CHECK (proposal_id IS NULL);
DROP TABLE content_media_bindings;
DROP INDEX idx_content_assets_proposal_revision;
DROP INDEX idx_content_assets_material_revision;
ALTER TABLE content_assets DROP COLUMN proposal_revision_id, DROP COLUMN proposal_id;
ALTER TABLE content_change_proposal_revisions DROP COLUMN attachments_snapshot;
