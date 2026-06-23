package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/overmindv/bumblebee/internal/pkg/domain"
	"github.com/overmindv/bumblebee/internal/pkg/service"
)

type TemplateItemStore struct {
	db *sql.DB
}

func NewTemplateItemStore(db *sql.DB) *TemplateItemStore {
	return &TemplateItemStore{db: db}
}

func (s *TemplateItemStore) PingContext(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *TemplateItemStore) Create(ctx context.Context, input domain.CreateTemplateItemInput) (domain.TemplateItem, error) {
	const query = `
		INSERT INTO template_items (id, name, description, status)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, description, status, created_at, updated_at
	`

	item := domain.TemplateItem{
		ID:          uuid.NewString(),
		Name:        input.Name,
		Description: input.Description,
		Status:      input.Status,
	}

	err := s.db.QueryRowContext(ctx, query, item.ID, item.Name, item.Description, item.Status).
		Scan(&item.ID, &item.Name, &item.Description, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		return domain.TemplateItem{}, err
	}

	return item, nil
}

func (s *TemplateItemStore) Get(ctx context.Context, id string) (domain.TemplateItem, error) {
	const query = `
		SELECT id, name, description, status, created_at, updated_at
		FROM template_items
		WHERE id = $1
	`

	var item domain.TemplateItem
	err := s.db.QueryRowContext(ctx, query, id).
		Scan(&item.ID, &item.Name, &item.Description, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TemplateItem{}, service.ErrNotFound
	}
	if err != nil {
		return domain.TemplateItem{}, err
	}

	return item, nil
}

func (s *TemplateItemStore) List(ctx context.Context) ([]domain.TemplateItem, error) {
	const query = `
		SELECT id, name, description, status, created_at, updated_at
		FROM template_items
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]domain.TemplateItem, 0)
	for rows.Next() {
		var item domain.TemplateItem
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Status, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}

		items = append(items, item)
	}

	return items, rows.Err()
}

func (s *TemplateItemStore) Update(ctx context.Context, input domain.UpdateTemplateItemInput) (domain.TemplateItem, error) {
	const query = `
		UPDATE template_items
		SET name = $2,
		    description = $3,
		    status = $4,
		    updated_at = NOW()
		WHERE id = $1
		RETURNING id, name, description, status, created_at, updated_at
	`

	var item domain.TemplateItem
	err := s.db.QueryRowContext(ctx, query, input.ID, input.Name, input.Description, input.Status).
		Scan(&item.ID, &item.Name, &item.Description, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TemplateItem{}, service.ErrNotFound
	}
	if err != nil {
		return domain.TemplateItem{}, err
	}

	return item, nil
}

func (s *TemplateItemStore) Delete(ctx context.Context, id string) (domain.TemplateItem, error) {
	const query = `
		DELETE FROM template_items
		WHERE id = $1
		RETURNING id, name, description, status, created_at, updated_at
	`

	var item domain.TemplateItem
	err := s.db.QueryRowContext(ctx, query, id).
		Scan(&item.ID, &item.Name, &item.Description, &item.Status, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.TemplateItem{}, service.ErrNotFound
	}
	if err != nil {
		return domain.TemplateItem{}, err
	}

	return item, nil
}
