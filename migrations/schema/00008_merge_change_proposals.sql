-- +goose Up
ALTER TABLE content_change_proposal_revisions
    ADD COLUMN base_revision_id UUID REFERENCES content_revisions (id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE content_change_proposals
    ADD COLUMN merged_revision_id UUID REFERENCES content_revisions (id) DEFERRABLE INITIALLY DEFERRED,
    DROP CONSTRAINT content_change_proposals_check,
    ADD CONSTRAINT content_change_proposals_state_check CHECK (
        (status = 'open' AND closed_at IS NULL AND merged_revision_id IS NULL)
        OR (status = 'closed' AND closed_at IS NOT NULL AND merged_revision_id IS NULL)
        OR (status = 'merged' AND closed_at IS NOT NULL AND merged_revision_id IS NOT NULL)
    );

CREATE UNIQUE INDEX uq_change_proposals_merged_revision
    ON content_change_proposals (merged_revision_id) WHERE merged_revision_id IS NOT NULL;
CREATE INDEX idx_proposal_revisions_base ON content_change_proposal_revisions (base_revision_id)
    WHERE base_revision_id IS NOT NULL;
CREATE INDEX idx_content_assets_revision ON content_assets (content_item_id, revision_id, position, id);

-- +goose Down
ALTER TABLE content_change_proposal_revisions ADD CONSTRAINT content_proposal_revisions_no_rebase_check
    CHECK (base_revision_id IS NULL);
ALTER TABLE content_change_proposals ADD CONSTRAINT content_change_proposals_check CHECK (
    (status = 'open' AND closed_at IS NULL) OR (status = 'closed' AND closed_at IS NOT NULL)
);
DROP INDEX idx_content_assets_revision;
DROP INDEX idx_proposal_revisions_base;
DROP INDEX uq_change_proposals_merged_revision;
ALTER TABLE content_change_proposals
    DROP CONSTRAINT content_change_proposals_state_check,
    DROP COLUMN merged_revision_id;
ALTER TABLE content_change_proposal_revisions DROP COLUMN base_revision_id;
