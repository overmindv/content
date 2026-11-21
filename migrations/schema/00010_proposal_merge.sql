-- +goose Up
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

CREATE TABLE content_proposal_merge_candidates (
    proposal_id UUID PRIMARY KEY,
    head_revision_id UUID NOT NULL,
    revision_id UUID NOT NULL UNIQUE REFERENCES content_media_bindings (revision_id),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (statement_timestamp() + INTERVAL '15 minutes'),
    FOREIGN KEY (proposal_id, head_revision_id)
        REFERENCES content_change_proposal_revisions (proposal_id, id) ON DELETE CASCADE
);
CREATE INDEX idx_proposal_merge_candidates_expiry ON content_proposal_merge_candidates (expires_at, proposal_id);

-- +goose Down
ALTER TABLE content_proposal_merge_candidates ADD CONSTRAINT no_pending_merges CHECK (FALSE);
ALTER TABLE content_change_proposals ADD CONSTRAINT content_change_proposals_check CHECK (
    (status = 'open' AND closed_at IS NULL) OR (status = 'closed' AND closed_at IS NOT NULL)
);
DROP TABLE content_proposal_merge_candidates;
DROP INDEX uq_change_proposals_merged_revision;
ALTER TABLE content_change_proposals
    DROP CONSTRAINT content_change_proposals_state_check,
    DROP COLUMN merged_revision_id;
