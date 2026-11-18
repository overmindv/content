-- +goose Up
CREATE TABLE content_change_review_threads (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    proposal_id UUID NOT NULL,
    revision_id UUID NOT NULL,
    line_number INTEGER NOT NULL CHECK (line_number >= 0),
    author_id UUID NOT NULL,
    body TEXT NOT NULL CHECK (octet_length(body) BETWEEN 1 AND 4096),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    resolved_by UUID,
    FOREIGN KEY (proposal_id, revision_id) REFERENCES content_change_proposal_revisions (proposal_id, id) ON DELETE CASCADE,
    CHECK ((resolved_at IS NULL) = (resolved_by IS NULL))
);

CREATE INDEX idx_change_review_threads_page ON content_change_review_threads (proposal_id, id);
CREATE INDEX idx_change_review_threads_revision ON content_change_review_threads (proposal_id, revision_id);

CREATE TABLE content_change_review_replies (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    thread_id BIGINT NOT NULL REFERENCES content_change_review_threads (id) ON DELETE CASCADE,
    author_id UUID NOT NULL,
    body TEXT NOT NULL CHECK (octet_length(body) BETWEEN 1 AND 4096),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_change_review_replies_page ON content_change_review_replies (thread_id, id);

CREATE TABLE content_change_review_decisions (
    proposal_id UUID NOT NULL,
    revision_id UUID NOT NULL,
    reviewer_id UUID NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('approve', 'request_changes')),
    reason TEXT NOT NULL CHECK (octet_length(reason) <= 4096),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (proposal_id, revision_id, reviewer_id),
    FOREIGN KEY (proposal_id, revision_id) REFERENCES content_change_proposal_revisions (proposal_id, id) ON DELETE CASCADE,
    CHECK (decision <> 'request_changes' OR reason <> '')
);

-- +goose Down
DROP TABLE content_change_review_decisions;
DROP TABLE content_change_review_replies;
DROP TABLE content_change_review_threads;
