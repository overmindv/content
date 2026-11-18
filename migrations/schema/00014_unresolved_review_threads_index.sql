-- +goose NO TRANSACTION
-- +goose Up
CREATE INDEX CONCURRENTLY idx_change_review_threads_unresolved
    ON content_change_review_threads (proposal_id)
    WHERE resolved_at IS NULL;

-- +goose Down
DROP INDEX CONCURRENTLY idx_change_review_threads_unresolved;
