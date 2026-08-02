-- +goose Up
CREATE TABLE IF NOT EXISTS content_items (
    id UUID PRIMARY KEY,
    type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft',
    title TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    current_revision_id UUID,
    published_revision_id UUID,
    created_by UUID,
    updated_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    CONSTRAINT content_items_type_check CHECK (type IN ('article', 'note', 'summary', 'theory')),
    CONSTRAINT content_items_status_check CHECK (status IN ('draft', 'published', 'archived')),
    CONSTRAINT content_items_slug_unique UNIQUE (slug)
);

CREATE TABLE IF NOT EXISTS content_revisions (
    id UUID PRIMARY KEY,
    content_item_id UUID NOT NULL REFERENCES content_items (id) ON DELETE CASCADE,
    revision INTEGER NOT NULL,
    format TEXT NOT NULL,
    source TEXT NOT NULL,
    source_hash TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT content_revisions_format_check CHECK (format IN ('markdown', 'typst')),
    CONSTRAINT content_revisions_item_revision_unique UNIQUE (content_item_id, revision)
);

ALTER TABLE content_items
    ADD CONSTRAINT content_items_current_revision_id_fkey
    FOREIGN KEY (current_revision_id) REFERENCES content_revisions (id) ON DELETE SET NULL;

ALTER TABLE content_items
    ADD CONSTRAINT content_items_published_revision_id_fkey
    FOREIGN KEY (published_revision_id) REFERENCES content_revisions (id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS tags (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT tags_slug_unique UNIQUE (slug)
);

CREATE TABLE IF NOT EXISTS content_tags (
    content_item_id UUID NOT NULL REFERENCES content_items (id) ON DELETE CASCADE,
    tag_id UUID NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (content_item_id, tag_id)
);

CREATE TABLE IF NOT EXISTS content_assets (
    id UUID PRIMARY KEY,
    content_item_id UUID NOT NULL REFERENCES content_items (id) ON DELETE CASCADE,
    revision_id UUID REFERENCES content_revisions (id) ON DELETE CASCADE,
    asset_id UUID NOT NULL,
    kind TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    position INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT content_assets_kind_check CHECK (kind IN ('image', 'attachment', 'pdf', 'archive'))
);

CREATE INDEX IF NOT EXISTS idx_content_items_type ON content_items (type);
CREATE INDEX IF NOT EXISTS idx_content_items_status ON content_items (status);
CREATE INDEX IF NOT EXISTS idx_content_items_created_at ON content_items (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_content_revisions_content_item_id ON content_revisions (content_item_id);
CREATE INDEX IF NOT EXISTS idx_content_assets_content_item_id ON content_assets (content_item_id);

-- +goose Down
DROP TABLE IF EXISTS content_assets;
DROP TABLE IF EXISTS content_tags;
DROP TABLE IF EXISTS tags;

ALTER TABLE content_items DROP CONSTRAINT IF EXISTS content_items_published_revision_id_fkey;
ALTER TABLE content_items DROP CONSTRAINT IF EXISTS content_items_current_revision_id_fkey;

DROP TABLE IF EXISTS content_revisions;
DROP TABLE IF EXISTS content_items;
