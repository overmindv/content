package validator

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/overmindv/content/internal/pkg/domain"
)

func photoBlock(ids ...string) string {
	return "```ovm-photos\n" + strings.Join(ids, "\n") + "\n```\n"
}

func TestPublishablePhotoIDs(t *testing.T) {
	t.Parallel()
	first, second := uuid.NewString(), uuid.NewString()
	source := "Before\r\n\r\n" + strings.ReplaceAll(photoBlock(first, "   "+second), "\n", "\r\n") +
		"\r\nBetween\n\n" + photoBlock(first) + "\nAfter"
	got, err := PublishablePhotoIDs(domain.ContentFormatMarkdown, source)
	if err != nil || !slices.Equal(got, []string{first, second}) {
		t.Error("photo IDs in order of first occurrence", got, err)
	}
	literal := "Example:\n\n````markdown\n" + photoBlock("not a UUID") + "````\n"
	got, err = PublishablePhotoIDs(domain.ContentFormatMarkdown, literal)
	if err != nil || len(got) != 0 {
		t.Error("code example was treated as a gallery", got, err)
	}
}

func TestRejectInvalidPhotoBlocks(t *testing.T) {
	t.Parallel()
	id := "a0123456-0000-4000-8000-000000000001"
	large := make([]string, maxPhotosPerBlock+1)
	for i := range large {
		large[i] = uuid.NewString()
	}
	for _, tc := range []struct{ name, source string }{
		{"empty block", "```ovm-photos\n```\n"},
		{"URL", photoBlock("https://cdn.example.com/image.jpg")},
		{"path", photoBlock("../image.jpg")},
		{"nil UUID", photoBlock(uuid.Nil.String())},
		{"uppercase", photoBlock(strings.ToUpper(id))},
		{"duplicate in group", photoBlock(id, id)},
		{"too many in a group", photoBlock(large...)},
		{"too many across groups", strings.Repeat(photoBlock(id)+"\n", MaxAssets+1)},
		{"invalid after a valid group", photoBlock(id) + "\n" + photoBlock("not a UUID")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := PublishablePhotoIDs(domain.ContentFormatMarkdown, tc.source)
			if !IsValidationError(err) || len(got) != 0 {
				t.Error("accepted invalid photo block", got, err)
			}
		})
	}
	if err := ValidatePublishableSource(domain.ContentFormatMarkdown, strings.Repeat(photoBlock(id)+"\n", MaxAssets)); err != nil {
		t.Error("limit must allow exactly 32 references", err)
	}
}

func BenchmarkValidatePhotoGroups(b *testing.B) {
	ids := make([]string, MaxAssets)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	source := strings.Repeat("Text and examples.\n\n", 300) +
		photoBlock(ids[:10]...) + "\n" + photoBlock(ids[10:20]...) + "\n" +
		photoBlock(ids[20:30]...) + "\n" + photoBlock(ids[30:]...)
	b.SetBytes(int64(len(source)))
	b.ReportAllocs()
	for b.Loop() {
		if err := ValidatePublishableSource(domain.ContentFormatMarkdown, source); err != nil {
			b.Fatal(err)
		}
	}
}
