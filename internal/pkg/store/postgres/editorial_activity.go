package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/pkg/api/activity"
)

func (s *ContentItemStore) enqueueEditorialActivity(
	ctx context.Context, tx pgx.Tx, item domain.ContentItem, version int64, proposalID string, proposal proposalMerge, review domain.ReviewRevisionStatus,
) error {
	topic := s.activity.contribution
	if topic == "" {
		return nil
	}
	payload := activity.EditorialContribution{
		ProposalID: proposalID, ProposalRevisionID: proposal.headID, AuthorID: proposal.authorID, TopicID: review.TopicID,
	}
	events := make([]activity.Event[activity.EditorialContribution], 0, 1+len(review.Assignments))
	events = append(events, newActivity(activity.ContributionMerged, item.ID, item.PublishedRevisionID, version, payload))
	for _, assignment := range review.Assignments {
		if assignment.Decision == "" || assignment.ReviewerID == proposal.authorID {
			continue
		}
		payload.ReviewerID, payload.Decision = assignment.ReviewerID, string(assignment.Decision)
		events = append(events, newActivity(activity.ReviewAccepted, item.ID, item.PublishedRevisionID, version, payload))
	}
	ids, values := make([]string, len(events)), make([][]byte, len(events))
	for index, event := range events {
		value, err := json.Marshal(event)
		if err != nil {
			return err
		}
		ids[index], values[index] = event.EventID, value
	}
	const insert = `
		INSERT INTO outbox (id, topic, message_key, message_value)
		SELECT id, $1, $2, value FROM unnest($3::uuid[], $4::bytea[]) AS event (id, value)`
	if _, err := tx.Exec(ctx, insert, topic, item.ID, ids, values); err != nil {
		return fmt.Errorf("enqueue editorial activity: %w", err)
	}

	return nil
}
