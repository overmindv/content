package content

import (
	"context"

	"github.com/overmindv/content/internal/app/content/mapper"
	contentpb "github.com/overmindv/content/internal/pkg/api/content"
	"github.com/overmindv/content/internal/pkg/domain"
)

func (s *Server) OpenChangeProposal(ctx context.Context, req *contentpb.OpenChangeProposalRequest) (*contentpb.OpenChangeProposalResponse, error) {
	actor, err := actorID(ctx)
	if err != nil {
		return nil, err
	}
	proposal, err := s.proposals.Open(ctx, domain.OpenChangeProposalInput{
		ContentItemID: req.GetContentItemId(), BaseRevisionID: req.GetBaseRevisionId(),
		AuthorID: actor, Source: req.GetSource(), Message: req.GetMessage(),
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
