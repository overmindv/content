package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
	"github.com/overmindv/content/internal/pkg/validator"
)

const reviewThreadColumns = `id, revision_id, line_number, author_id, body, created_at, resolved_at, resolved_by`
const reviewReplyColumns = `id, author_id, body, created_at`

func (s *ContentItemStore) CreateReviewThread(ctx context.Context, input domain.CreateReviewThreadInput) (domain.ReviewThread, error) {
	return writeReview(ctx, s, input.ReviewActor, func(tx pgx.Tx, _ reviewScope) (domain.ReviewThread, error) {
		const revision = `
			SELECT CASE WHEN $3::integer > 0 THEN source ELSE '' END
			FROM content_change_proposal_revisions WHERE proposal_id = $1 AND id = $2
		`
		var source string
		err := tx.QueryRow(ctx, revision, input.ProposalID, input.RevisionID, input.LineNumber).Scan(&source)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ReviewThread{}, service.ErrProposalRevisionNotFound
		}
		if err != nil {
			return domain.ReviewThread{}, err
		}
		lines := strings.Count(source, "\n")
		if source != "" && !strings.HasSuffix(source, "\n") {
			lines++
		}
		if int(input.LineNumber) > lines {
			return domain.ReviewThread{}, validator.Error{Message: "line_number is outside the revision"}
		}
		const insert = `
			INSERT INTO content_change_review_threads (proposal_id, revision_id, line_number, author_id, body)
			VALUES ($1, $2, $3, $4, $5) RETURNING ` + reviewThreadColumns
		return scanReviewThread(tx.QueryRow(ctx, insert, input.ProposalID, input.RevisionID, input.LineNumber, input.ActorID, input.Body))
	})
}

func (s *ContentItemStore) ReplyToReviewThread(ctx context.Context, ref domain.ReviewThreadRef, body string) (domain.ReviewReply, error) {
	return writeReview(ctx, s, ref.ReviewActor, func(tx pgx.Tx, _ reviewScope) (domain.ReviewReply, error) {
		const insert = `
			INSERT INTO content_change_review_replies (thread_id, author_id, body)
			SELECT id, $3, $4 FROM content_change_review_threads WHERE proposal_id = $1 AND id = $2
			RETURNING ` + reviewReplyColumns
		reply, err := scanReviewReply(tx.QueryRow(ctx, insert, ref.ProposalID, ref.ThreadID, ref.ActorID, body))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ReviewReply{}, service.ErrReviewThreadNotFound
		}
		return reply, err
	})
}

func (s *ContentItemStore) SetReviewThreadResolved(ctx context.Context, ref domain.ReviewThreadRef, resolved bool) (domain.ReviewThread, error) {
	return writeReview(ctx, s, ref.ReviewActor, func(tx pgx.Tx, _ reviewScope) (domain.ReviewThread, error) {
		const update = `
			UPDATE content_change_review_threads
			SET resolved_at = CASE WHEN $4::boolean THEN COALESCE(resolved_at, NOW()) END,
			    resolved_by = CASE WHEN $4 THEN COALESCE(resolved_by, $3) END
			WHERE proposal_id = $1 AND id = $2 RETURNING ` + reviewThreadColumns
		thread, err := scanReviewThread(tx.QueryRow(ctx, update, ref.ProposalID, ref.ThreadID, ref.ActorID, resolved))
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ReviewThread{}, service.ErrReviewThreadNotFound
		}
		return thread, err
	})
}

func (s *ContentItemStore) ListReviewThreads(ctx context.Context, input domain.ReviewPageInput) (domain.ReviewPage[domain.ReviewThread], error) {
	if _, err := loadReviewScope(ctx, s.db, input.ReviewActor, false); err != nil {
		return domain.ReviewPage[domain.ReviewThread]{}, err
	}
	const query = `SELECT ` + reviewThreadColumns + `
		FROM content_change_review_threads WHERE proposal_id = $1 AND id > $2 ORDER BY id LIMIT $3`
	rows, err := s.db.Query(ctx, query, input.ProposalID, input.AfterID, input.PageSize+1)
	if err != nil {
		return domain.ReviewPage[domain.ReviewThread]{}, err
	}
	return collectReviewPage(rows, input.PageSize, scanReviewThread, func(t domain.ReviewThread) int64 { return t.ID })
}

func (s *ContentItemStore) ListReviewReplies(ctx context.Context, input domain.ReviewReplyPageInput) (domain.ReviewPage[domain.ReviewReply], error) {
	if _, err := loadReviewScope(ctx, s.db, input.ReviewActor, false); err != nil {
		return domain.ReviewPage[domain.ReviewReply]{}, err
	}
	const threadQuery = `SELECT id FROM content_change_review_threads WHERE proposal_id = $1 AND id = $2`
	var threadID int64
	err := s.db.QueryRow(ctx, threadQuery, input.ProposalID, input.ThreadID).Scan(&threadID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ReviewPage[domain.ReviewReply]{}, service.ErrReviewThreadNotFound
	}
	if err != nil {
		return domain.ReviewPage[domain.ReviewReply]{}, err
	}
	const query = `SELECT ` + reviewReplyColumns + `
		FROM content_change_review_replies WHERE thread_id = $1 AND id > $2 ORDER BY id LIMIT $3`
	rows, err := s.db.Query(ctx, query, threadID, input.AfterID, input.PageSize+1)
	if err != nil {
		return domain.ReviewPage[domain.ReviewReply]{}, err
	}
	return collectReviewPage(rows, input.PageSize, scanReviewReply, func(r domain.ReviewReply) int64 { return r.ID })
}

func collectReviewPage[T any](rows pgx.Rows, size int32, scan func(scanner) (T, error), id func(T) int64) (domain.ReviewPage[T], error) {
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (T, error) { return scan(row) })
	if err != nil {
		return domain.ReviewPage[T]{}, err
	}
	page := domain.ReviewPage[T]{Items: items}
	if len(items) > int(size) {
		page.Items = items[:size]
		page.NextAfterID = id(page.Items[len(page.Items)-1])
	}
	return page, nil
}

func scanReviewThread(row scanner) (domain.ReviewThread, error) {
	var thread domain.ReviewThread
	var resolvedBy *string
	err := row.Scan(&thread.ID, &thread.RevisionID, &thread.LineNumber, &thread.AuthorID, &thread.Body, &thread.CreatedAt, &thread.ResolvedAt, &resolvedBy)
	thread.ResolvedBy = stringFromPtr(resolvedBy)
	return thread, err
}

func scanReviewReply(row scanner) (domain.ReviewReply, error) {
	var reply domain.ReviewReply
	err := row.Scan(&reply.ID, &reply.AuthorID, &reply.Body, &reply.CreatedAt)
	return reply, err
}
