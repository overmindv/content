-- +goose Up
ALTER TABLE content_change_proposal_revisions
    ADD COLUMN base_revision_id UUID REFERENCES content_revisions (id) DEFERRABLE INITIALLY DEFERRED;

CREATE INDEX idx_proposal_revisions_base ON content_change_proposal_revisions (base_revision_id)
    WHERE base_revision_id IS NOT NULL;

-- +goose Down
ALTER TABLE content_change_proposal_revisions ADD CONSTRAINT no_rebased_proposal_revisions
    CHECK (base_revision_id IS NULL);
DROP INDEX idx_proposal_revisions_base;
ALTER TABLE content_change_proposal_revisions DROP COLUMN base_revision_id;
