package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/overmindv/content/internal/mock"
	"github.com/overmindv/content/internal/pkg/domain"
	"go.uber.org/mock/gomock"
)

func TestUnifiedDiff(t *testing.T) {
	t.Parallel()

	const header = "--- base\n+++ revision\n"
	for _, tt := range []struct{ name, base, target, want string }{
		{"unicode", "Заголовок\nслово\n", "Заголовок\nфраза\n", header + "@@ -1,2 +1,2 @@\n Заголовок\n-слово\n+фраза\n"},
		{"empty base", "", "first\n", header + "@@ -0,0 +1,1 @@\n+first\n"},
		{"empty target", "first\n", "", header + "@@ -1,1 +0,0 @@\n-first\n"},
		{"no trailing newline", "text", "text\n", header + "@@ -1,1 +1,1 @@\n-text\n\\ No newline at end of file\n+text\n"},
		{"no changes", "text\n", "text\n", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, truncated, err := makeUnifiedDiff(context.Background(), tt.base, tt.target, proposalDiffBudget)
			if err != nil || truncated || got != tt.want {
				t.Errorf("diff = %q, truncated = %v, error = %v; want %q", got, truncated, err, tt.want)
			}
		})
	}
}

func TestUnifiedDiffLimits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	contextLines := "a\nb\nc\nd\ne\nf\ng\nh\n"
	base := "old\n" + contextLines + "before\n"
	larger := "new\n" + contextLines + strings.Repeat("ы", maxProposalDiffBytes) + "\n"
	patch, truncated, err := makeUnifiedDiff(ctx, base, larger, proposalDiffBudget)
	if err != nil || !truncated || len(patch) > maxProposalDiffBytes || !utf8.ValidString(patch) ||
		!strings.Contains(patch, "-old\n+new\n") || strings.Count(patch, "@@ -") != 1 {
		t.Errorf("truncated diff = %q, truncated = %v, error = %v; want only the first complete hunk", patch, truncated, err)
	}
	patch, truncated, err = makeUnifiedDiff(ctx, base, "new\n"+contextLines+"after\n", proposalDiffBudget)
	if err != nil || truncated || !strings.Contains(patch, "@@ -7,4 +7,4 @@\n") {
		t.Errorf("second hunk coordinates: %q, %v, %v", patch, truncated, err)
	}
	patch, truncated, err = makeUnifiedDiff(ctx, base, strings.Repeat("x\n", maxProposalDiffLines), proposalDiffBudget)
	if err != nil || !truncated || patch != "" {
		t.Errorf("line limit = %q, %v, %v", patch, truncated, err)
	}
	patch, truncated, err = makeUnifiedDiff(ctx, "old", "new", 0)
	if err != nil || !truncated || patch != "" {
		t.Errorf("time budget = %q, %v, %v", patch, truncated, err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err = makeUnifiedDiff(canceled, "old", "new", proposalDiffBudget); !errors.Is(err, context.Canceled) {
		t.Errorf("canceled diff returned %v", err)
	}
}

func TestProposalDiffAdmission(t *testing.T) {
	t.Parallel()

	svc := NewChangeProposalService(nil)
	for range cap(svc.diffSlots) {
		svc.diffSlots <- struct{}{}
	}
	_, err := svc.Diff(context.Background(), domain.ChangeProposalDiffInput{ProposalID: "11111111-1111-4111-8111-111111111111"})
	if !errors.Is(err, ErrProposalDiffBusy) {
		t.Errorf("diff with all slots occupied returned %v", err)
	}
}

func TestProposalDiffSourceTimeoutReleasesSlot(t *testing.T) {
	t.Parallel()

	store := mock.NewMockChangeProposalStore(gomock.NewController(t))
	svc := NewChangeProposalService(store)
	input := domain.ChangeProposalDiffInput{ProposalID: "11111111-1111-4111-8111-111111111111"}
	store.EXPECT().GetChangeProposalDiffSources(gomock.Any(), input).
		DoAndReturn(func(ctx context.Context, _ domain.ChangeProposalDiffInput) (domain.ChangeProposalDiffSources, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > proposalDiffSourceTimeout {
				t.Error("diff source read must have a bounded deadline")
			}

			return domain.ChangeProposalDiffSources{}, context.DeadlineExceeded
		})
	if _, err := svc.Diff(t.Context(), input); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("diff after source timeout = %v, want DeadlineExceeded", err)
	}
	if len(svc.diffSlots) != 0 {
		t.Error("diff slot was not released")
	}
}

func BenchmarkProposalUnifiedDiff(b *testing.B) {
	const lines = 4000
	var base, target strings.Builder
	for i := range lines {
		fmt.Fprintf(&base, "line %04d: old\n", i)
		if i%50 == 0 {
			fmt.Fprintf(&target, "line %04d: new\n", i)
		} else {
			fmt.Fprintf(&target, "line %04d: old\n", i)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		patch, truncated, err := makeUnifiedDiff(b.Context(), base.String(), target.String(), proposalDiffBudget)
		if err != nil || truncated || patch == "" {
			b.Fatal("expected a complete diff")
		}
	}
}
