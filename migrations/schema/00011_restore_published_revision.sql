-- +goose Up
ALTER TABLE content_revisions
    ADD COLUMN restored_from_revision_id UUID REFERENCES content_revisions (id),
    ADD CONSTRAINT different_restore_source CHECK (restored_from_revision_id <> id);
CREATE INDEX idx_content_revisions_restore_source ON content_revisions (restored_from_revision_id)
    WHERE restored_from_revision_id IS NOT NULL;
CREATE INDEX idx_content_catalog_published_revision ON content_catalog_versions (material_id, revision_id)
    WHERE NOT tombstone;

-- +goose Down
DROP INDEX idx_content_catalog_published_revision;
ALTER TABLE content_revisions ADD CONSTRAINT no_restored_revisions CHECK (restored_from_revision_id IS NULL);
ALTER TABLE content_revisions DROP COLUMN restored_from_revision_id;
