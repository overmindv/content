-- +goose Up
INSERT INTO template_items (id, name, description, status)
VALUES (
    '11111111-1111-1111-1111-111111111111',
    'starter-template-item',
    'Seed record for verifying CRUD flow and grpc contracts.',
    'active'
)
ON CONFLICT (id) DO NOTHING;

-- +goose Down
DELETE FROM template_items WHERE id = '11111111-1111-1111-1111-111111111111';
