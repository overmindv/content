-- +goose Up
-- Transaction outbox для событий контента (DDL паркера parker.OutboxSchema):
-- события пишутся атомарно с бизнес-изменением и доставляются фоновым диспетчером.
CREATE TABLE IF NOT EXISTS outbox (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    topic           TEXT NOT NULL,
    message_key     TEXT NOT NULL,
    message_value   BYTEA NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    attempts        INT  NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at         TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS outbox_status_idx ON outbox(status, next_attempt_at);

-- +goose Down
DROP TABLE IF EXISTS outbox;
