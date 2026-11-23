package mapper

import (
	"github.com/overmindv/content/internal/pkg/domain"
	contentpb "github.com/overmindv/content/pkg/api/content"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func ToProtoChangeProposal(proposal domain.ChangeProposal) *contentpb.ChangeProposal {
	return &contentpb.ChangeProposal{
		Id:                 proposal.ID,
		ContentItemId:      proposal.ContentItemID,
		BaseRevisionId:     proposal.BaseRevisionID,
		MergedRevisionId:   proposal.MergedRevisionID,
		BaseOutdated:       proposal.BaseOutdated,
		InitialPublication: proposal.InitialPublication,
		AuthorId:           proposal.AuthorID,
		Status:             string(proposal.Status),
		CreatedAt:          timestamppb.New(proposal.CreatedAt),
		ClosedAt:           timestampOrNil(proposal.ClosedAt),
		Head: &contentpb.ChangeProposalRevision{
			Id:               proposal.Head.ID,
			ParentRevisionId: proposal.Head.ParentRevisionID,
			Ordinal:          proposal.Head.Ordinal,
			Source:           proposal.Head.Source,
			Message:          proposal.Head.Message,
			CreatedAt:        timestamppb.New(proposal.Head.CreatedAt),
			Assets:           ToProtoAssets(proposal.Head.Assets),
		},
	}
}

func ToProtoChangeProposalHistory(page domain.ChangeProposalHistory) *contentpb.ListChangeProposalHistoryResponse {
	entries := make([]*contentpb.ChangeProposalHistoryEntry, 0, len(page.Entries))
	for _, entry := range page.Entries {
		entries = append(entries, &contentpb.ChangeProposalHistoryEntry{
			Id: entry.ID, ParentRevisionId: entry.ParentRevisionID, BaseRevisionId: entry.BaseRevisionID, Ordinal: entry.Ordinal,
			AuthorId: entry.AuthorID, Message: entry.Message, CreatedAt: timestamppb.New(entry.CreatedAt),
		})
	}

	return &contentpb.ListChangeProposalHistoryResponse{Entries: entries, NextBeforeOrdinal: page.NextBeforeOrdinal}
}
