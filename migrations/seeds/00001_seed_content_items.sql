-- +goose Up
INSERT INTO content_items (
    id,
    type,
    status,
    title,
    slug,
    description,
    created_by,
    updated_by
)
VALUES (
    '11111111-1111-1111-1111-111111111111',
    'article',
    'published',
    'Starter content item',
    'starter-content-item',
    'Seed record for verifying content CRUD flow and grpc contracts.',
    NULL,
    NULL
)
ON CONFLICT (id) DO NOTHING;

INSERT INTO content_revisions (
    id,
    content_item_id,
    revision,
    format,
    source,
    source_hash,
    message,
    created_by
)
VALUES (
    '22222222-2222-2222-2222-222222222222',
    '11111111-1111-1111-1111-111111111111',
    1,
    'markdown',
    '# Starter content item',
    '97e8e5c5632ef5656de893c5acf20dd92f65623a86804fcae8950be7b3e67340',
    'initial seed',
    NULL
)
ON CONFLICT (content_item_id, revision) DO NOTHING;

UPDATE content_items
SET current_revision_id = '22222222-2222-2222-2222-222222222222',
    published_revision_id = '22222222-2222-2222-2222-222222222222',
    published_at = COALESCE(published_at, NOW())
WHERE id = '11111111-1111-1111-1111-111111111111';

INSERT INTO tags (id, name, slug)
VALUES ('33333333-3333-3333-3333-333333333333', 'starter', 'starter')
ON CONFLICT (slug) DO NOTHING;

INSERT INTO content_tags (content_item_id, tag_id)
VALUES (
    '11111111-1111-1111-1111-111111111111',
    '33333333-3333-3333-3333-333333333333'
)
ON CONFLICT (content_item_id, tag_id) DO NOTHING;

-- +goose Down
DELETE FROM content_items WHERE id = '11111111-1111-1111-1111-111111111111';
DELETE FROM tags WHERE id = '33333333-3333-3333-3333-333333333333';
