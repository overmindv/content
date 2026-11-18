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
	store ChangeProposalStore
}

func NewChangeProposalService(store ChangeProposalStore) *ChangeProposalService {
	return &ChangeProposalService{store: store}
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
