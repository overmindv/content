package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/validator"
)

type ChangeProposalService struct {
	store     ChangeProposalStore
	diffSlots chan struct{}
}

func NewChangeProposalService(store ChangeProposalStore) *ChangeProposalService {
	return &ChangeProposalService{store: store, diffSlots: make(chan struct{}, 4)}
}

func (s *ChangeProposalService) Open(ctx context.Context, input domain.OpenChangeProposalInput) (domain.ChangeProposal, error) {
	if !validProposalID(input.ContentItemID) || !validProposalID(input.BaseRevisionID) {
		return domain.ChangeProposal{}, validator.Error{Message: "content_item_id and base_revision_id must be UUIDs"}
	}
	if err := validateProposalRevision(input.Source, input.Message); err != nil {
		return domain.ChangeProposal{}, err
	}
	input.BaseRevisionID = strings.ToLower(input.BaseRevisionID)

	return s.store.OpenChangeProposal(ctx, input)
}

func (s *ChangeProposalService) Get(ctx context.Context, id, actorID string) (domain.ChangeProposal, error) {
	if !validProposalID(id) {
		return domain.ChangeProposal{}, validator.Error{Message: "proposal_id must be a UUID"}
	}

	return s.store.GetChangeProposal(ctx, id, actorID)
}

func (s *ChangeProposalService) Append(ctx context.Context, input domain.AppendChangeProposalRevisionInput) (domain.ChangeProposal, error) {
	if !validProposalID(input.ProposalID) || !validProposalID(input.ExpectedHeadID) {
		return domain.ChangeProposal{}, validator.Error{Message: "proposal_id and expected_head_id must be UUIDs"}
	}
	if err := validateProposalRevision(input.Source, input.Message); err != nil {
		return domain.ChangeProposal{}, err
	}
	input.ExpectedHeadID = strings.ToLower(input.ExpectedHeadID)

	return s.store.AppendChangeProposalRevision(ctx, input)
}

func (s *ChangeProposalService) Close(ctx context.Context, id, actorID string) (domain.ChangeProposal, error) {
	if !validProposalID(id) {
		return domain.ChangeProposal{}, validator.Error{Message: "proposal_id must be a UUID"}
	}

	return s.store.CloseChangeProposal(ctx, id, actorID)
}

func (s *ChangeProposalService) History(ctx context.Context, input domain.ChangeProposalHistoryInput) (domain.ChangeProposalHistory, error) {
	if !validProposalID(input.ProposalID) {
		return domain.ChangeProposalHistory{}, validator.Error{Message: "proposal_id must be a UUID"}
	}
	if input.BeforeOrdinal < 0 || input.PageSize < 0 || input.PageSize > 50 {
		return domain.ChangeProposalHistory{}, validator.Error{Message: "invalid history cursor or page size (maximum 50)"}
	}
	if input.PageSize == 0 {
		input.PageSize = 20
	}

	return s.store.ListChangeProposalHistory(ctx, input)
}

func (s *ChangeProposalService) Diff(ctx context.Context, input domain.ChangeProposalDiffInput) (domain.ChangeProposalDiff, error) {
	if !validProposalID(input.ProposalID) || input.RevisionID != "" && !validProposalID(input.RevisionID) {
		return domain.ChangeProposalDiff{}, validator.Error{Message: "proposal_id and optional revision_id must be UUIDs"}
	}
	select {
	case <-ctx.Done():
		return domain.ChangeProposalDiff{}, ctx.Err()
	case s.diffSlots <- struct{}{}:
		defer func() { <-s.diffSlots }()
	default:
		return domain.ChangeProposalDiff{}, ErrProposalDiffBusy
	}
	readCtx, cancel := context.WithTimeout(ctx, proposalDiffSourceTimeout)
	sources, err := s.store.GetChangeProposalDiffSources(readCtx, input)
	cancel()
	if err != nil {
		return domain.ChangeProposalDiff{}, err
	}
	result := domain.ChangeProposalDiff{BaseRevisionID: sources.BaseRevisionID, RevisionID: sources.RevisionID, Truncated: sources.Oversized}
	if !sources.Oversized {
		result.UnifiedDiff, result.Truncated, err = makeUnifiedDiff(ctx, sources.BaseSource, sources.Source, proposalDiffBudget)
	}

	return result, err
}

func validProposalID(id string) bool {
	parsed, err := uuid.Parse(id)

	return len(id) == 36 && err == nil && parsed != uuid.Nil
}

func validateProposalRevision(source, message string) error {
	const maxMessageBytes = 1024
	if len(message) > maxMessageBytes || !utf8.ValidString(message) || strings.IndexByte(message, 0) >= 0 {
		return validator.Error{Message: "message must be UTF-8 text without NUL, up to 1024 bytes"}
	}

	return validator.ValidatePublishableSource(domain.ContentFormatMarkdown, source)
}
