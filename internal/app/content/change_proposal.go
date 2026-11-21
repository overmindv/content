package content

import (
	"context"

	"github.com/overmindv/content/internal/app/content/mapper"
	"github.com/overmindv/content/internal/pkg/domain"
	contentpb "github.com/overmindv/content/pkg/api/content"
)

func (s *Server) OpenChangeProposal(ctx context.Context, req *contentpb.OpenChangeProposalRequest) (*contentpb.OpenChangeProposalResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	proposal, err := s.proposals.Open(ctx, domain.OpenChangeProposalInput{
		ContentItemID: req.GetContentItemId(), BaseRevisionID: req.GetBaseRevisionId(),
		AuthorID: actor, Source: req.GetSource(), Message: req.GetMessage(),
		Attachments: mapper.FromAttachmentSet(req.GetAttachments()),
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.OpenChangeProposalResponse{Proposal: mapper.ToProtoChangeProposal(proposal)}, nil
}

func (s *Server) GetChangeProposal(ctx context.Context, req *contentpb.GetChangeProposalRequest) (*contentpb.GetChangeProposalResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	proposal, err := s.proposals.Get(ctx, req.GetProposalId(), actor)
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.GetChangeProposalResponse{Proposal: mapper.ToProtoChangeProposal(proposal)}, nil
}

func (s *Server) AppendChangeProposalRevision(ctx context.Context, req *contentpb.AppendChangeProposalRevisionRequest) (*contentpb.AppendChangeProposalRevisionResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	proposal, err := s.proposals.Append(ctx, domain.AppendChangeProposalRevisionInput{
		ProposalID: req.GetProposalId(), ExpectedHeadID: req.GetExpectedHeadId(),
		AuthorID: actor, Source: req.GetSource(), Message: req.GetMessage(),
		Attachments: mapper.FromAttachmentSet(req.GetAttachments()),
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.AppendChangeProposalRevisionResponse{Proposal: mapper.ToProtoChangeProposal(proposal)}, nil
}

func (s *Server) CloseChangeProposal(ctx context.Context, req *contentpb.CloseChangeProposalRequest) (*contentpb.CloseChangeProposalResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	proposal, err := s.proposals.Close(ctx, req.GetProposalId(), actor)
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.CloseChangeProposalResponse{Proposal: mapper.ToProtoChangeProposal(proposal)}, nil
}

func (s *Server) RebaseChangeProposal(ctx context.Context, req *contentpb.RebaseChangeProposalRequest) (*contentpb.RebaseChangeProposalResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	proposal, err := s.proposals.Rebase(ctx, domain.RebaseChangeProposalInput{
		AppendChangeProposalRevisionInput: domain.AppendChangeProposalRevisionInput{
			ProposalID: req.GetProposalId(), ExpectedHeadID: req.GetExpectedHeadId(),
			AuthorID: actor, Source: req.GetSource(), Message: req.GetMessage(),
			Attachments: mapper.FromAttachmentSet(req.GetAttachments()),
		},
		BaseRevisionID: req.GetBaseRevisionId(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.RebaseChangeProposalResponse{Proposal: mapper.ToProtoChangeProposal(proposal)}, nil
}

func (s *Server) ListChangeProposalHistory(ctx context.Context, req *contentpb.ListChangeProposalHistoryRequest) (*contentpb.ListChangeProposalHistoryResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	page, err := s.proposals.History(ctx, domain.ChangeProposalHistoryInput{
		ProposalID: req.GetProposalId(), ActorID: actor, BeforeOrdinal: req.GetBeforeOrdinal(), PageSize: req.GetPageSize(),
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	return mapper.ToProtoChangeProposalHistory(page), nil
}

func (s *Server) GetChangeProposalDiff(ctx context.Context, req *contentpb.GetChangeProposalDiffRequest) (*contentpb.GetChangeProposalDiffResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	diff, err := s.proposals.Diff(ctx, domain.ChangeProposalDiffInput{
		ProposalID: req.GetProposalId(), RevisionID: req.GetRevisionId(), ActorID: actor,
	})
	if err != nil {
		return nil, toStatusError(err)
	}

	return &contentpb.GetChangeProposalDiffResponse{
		BaseRevisionId: diff.BaseRevisionID, RevisionId: diff.RevisionID, UnifiedDiff: diff.UnifiedDiff, Truncated: diff.Truncated,
	}, nil
}
