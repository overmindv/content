//go:build component

package component

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/overmindv/content/internal/pkg/service"
)

func TestRevisionHistoryUsesOwnerAndCursor(t *testing.T) {
	f := newActivityFixture(t)
	items := service.NewContentItemService(f.store, nil)
	material := f.state(t, f.create(t), domain.ContentStatusPublished)
	head := material.CurrentRevisionID
	for i := 2; i <= 5; i++ {
		revision, err := f.store.CreateRevision(f.ctx, domain.CreateContentRevisionInput{
			ContentItemID: material.ID, CreatedBy: f.owner, ExpectedRevisionID: head,
			Format: domain.ContentFormatMarkdown, Source: "# Private history", Message: strings.Repeat("x", 300),
		})
		if err != nil {
			t.Fatal(err)
		}
		head = revision.ID
	}
	input := domain.RevisionHistoryInput{ContentItemID: material.ID, ActorID: uuid.NewString(), PageSize: 2}
	if _, _, err := items.RevisionHistory(f.ctx, input); !errors.Is(err, service.ErrNotFound) {
		t.Errorf("reader accessed unpublished history: %v", err)
	}
	input.ActorID = f.owner
	var numbers []int
	for range 3 {
		page, next, err := items.RevisionHistory(f.ctx, input)
		if err != nil {
			t.Fatal(err)
		}
		for _, revision := range page {
			numbers = append(numbers, revision.Revision)
			if revision.Source != "" || len(revision.Message) > 256 {
				t.Error("history response contains source or unbounded metadata")
			}
		}
		input.BeforeRevision = next
	}
	if input.BeforeRevision != 0 || !reflect.DeepEqual(numbers, []int{5, 4, 3, 2, 1}) {
		t.Errorf("unexpected history across pages: %v, cursor %d", numbers, input.BeforeRevision)
	}
	input.BeforeRevision = 1
	page, cursor, err := items.RevisionHistory(f.ctx, input)
	if err != nil || cursor != 0 || len(page) != 0 {
		t.Errorf("expected empty final page for owner: %d items, cursor %d, err=%v", len(page), cursor, err)
	}
}
