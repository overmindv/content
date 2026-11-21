-- +goose Up
ALTER TABLE content_change_proposal_revisions ADD COLUMN review_topic_id UUID;

CREATE TABLE content_change_review_assignments (
    proposal_id UUID NOT NULL,
    revision_id UUID NOT NULL,
    reviewer_id UUID NOT NULL,
    slot SMALLINT NOT NULL CHECK (slot BETWEEN 1 AND 3),
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at TIMESTAMPTZ,
    decision TEXT CHECK (decision IN ('approve', 'request_changes')),
    reason TEXT CHECK (octet_length(reason) <= 4096),
    decided_at TIMESTAMPTZ,
    PRIMARY KEY (proposal_id, revision_id, reviewer_id),
    FOREIGN KEY (proposal_id, revision_id) REFERENCES content_change_proposal_revisions (proposal_id, id) ON DELETE CASCADE,
    CHECK ((decision IS NULL) = (decided_at IS NULL)),
    CHECK ((decision IS NULL) = (reason IS NULL)),
    CHECK (decision <> 'request_changes' OR reason <> '')
);

CREATE UNIQUE INDEX idx_change_review_active_slots
    ON content_change_review_assignments (proposal_id, revision_id, slot) WHERE revoked_at IS NULL;

-- +goose Down
DROP TABLE content_change_review_assignments;
ALTER TABLE content_change_proposal_revisions DROP COLUMN review_topic_id;
