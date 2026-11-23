-- +goose Up
ALTER TABLE content_change_proposals
    ADD COLUMN IF NOT EXISTS initial_publication BOOLEAN NOT NULL DEFAULT FALSE;

CREATE UNIQUE INDEX IF NOT EXISTS uq_initial_publication_merged
    ON content_change_proposals (content_item_id)
    WHERE initial_publication AND status = 'merged';

-- +goose Down
-- Сохраняем признак первой публикации и историю ревью при откате кода.
SELECT 1;
