-- +goose Up
CREATE TABLE content_change_proposals (
    id UUID PRIMARY KEY,
    content_item_id UUID NOT NULL REFERENCES content_items (id) ON DELETE CASCADE,
    base_revision_id UUID NOT NULL REFERENCES content_revisions (id),
    head_revision_id UUID NOT NULL,
    author_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    closed_at TIMESTAMPTZ,
    CHECK ((status = 'open' AND closed_at IS NULL) OR (status = 'closed' AND closed_at IS NOT NULL))
);

CREATE UNIQUE INDEX uq_open_change_proposal
    ON content_change_proposals (content_item_id, author_id) WHERE status = 'open';
CREATE INDEX idx_change_proposals_content_item ON content_change_proposals (content_item_id);
CREATE INDEX idx_change_proposals_base_revision ON content_change_proposals (base_revision_id);

CREATE TABLE content_change_proposal_revisions (
    proposal_id UUID NOT NULL REFERENCES content_change_proposals (id) ON DELETE CASCADE,
    id UUID NOT NULL,
    parent_revision_id UUID,
    ordinal INTEGER NOT NULL CHECK (ordinal > 0),
    source TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (proposal_id, id),
    UNIQUE (proposal_id, ordinal),
    FOREIGN KEY (proposal_id, parent_revision_id) REFERENCES content_change_proposal_revisions (proposal_id, id),
    CHECK ((ordinal = 1) = (parent_revision_id IS NULL))
);

ALTER TABLE content_change_proposals ADD CONSTRAINT fk_change_proposal_head
    FOREIGN KEY (id, head_revision_id) REFERENCES content_change_proposal_revisions (proposal_id, id)
    DEFERRABLE INITIALLY DEFERRED;

-- +goose Down
ALTER TABLE content_change_proposals DROP CONSTRAINT fk_change_proposal_head;
DROP TABLE content_change_proposal_revisions;
DROP TABLE content_change_proposals;
