-- +goose Up
ALTER TABLE content_change_proposal_revisions
    ADD COLUMN review_due_at TIMESTAMPTZ,
    ADD COLUMN review_check_token UUID,
    ADD COLUMN review_verified_until TIMESTAMPTZ;

CREATE INDEX idx_change_review_due
    ON content_change_proposal_revisions (review_due_at, proposal_id, id) WHERE review_due_at IS NOT NULL;

-- +goose Down
DROP INDEX idx_change_review_due;
ALTER TABLE content_change_proposal_revisions
    DROP COLUMN review_verified_until,
    DROP COLUMN review_check_token,
    DROP COLUMN review_due_at;
