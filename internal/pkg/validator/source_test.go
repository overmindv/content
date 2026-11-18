package validator

import (
	"strings"
	"testing"

	"github.com/overmindv/content/internal/pkg/domain"
)

func TestValidateMarkdownSource(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		valid  bool
	}{
		{"headings and links", "# Go\n\n[Ссылка](https://example.org/doc) и [раздел](#go)", true},
		{"code example", "```html\n<div>Пример</div>\n```\n\n`<div>`", true},
		{"inline html", "Текст <i>пример</i>", false},
		{"html block", "<div>Пример</div>", false},
		{"inline image", "![Схема](https://example.org/diagram.png)", false},
		{"https autolink", "https://example.org/doc", true},
		{"http autolink", "http://example.org/doc", false},
		{"non-https reference", "[Ссылка][doc]\n\n[doc]: ftp://example.org/doc", false},
		{"https reference", "[Ссылка][doc]\n\n[doc]: https://example.org/doc", true},
		{"relative link", "[Ссылка](//example.org/doc)", false},
		{"link credentials", "[Ссылка](https://example-user@example.org/doc)", false},
		{"missing host", "[Ссылка](https:doc)", false},
		{"url over limit", "[Ссылка](https://example.org/" + strings.Repeat("x", maxLinkBytes) + ")", false},
		{"link count at limit", strings.Repeat("[Ссылка](#go)\n", maxLinks), true},
		{"link count over limit", strings.Repeat("[Ссылка](#go)\n", maxLinks+1), false},
		{"source at byte limit", strings.Repeat("я", MaxSourceBytes/2), true},
		{"source over byte limit", strings.Repeat("я", MaxSourceBytes/2+1), false},
		{"nul", "часть\x00материала", false},
		{"invalid utf8", "\xff", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSource(domain.ContentFormatMarkdown, tt.source)
			if tt.valid && err != nil || !tt.valid && !IsValidationError(err) {
				t.Errorf("validateSource error = %v, valid = %t", err, tt.valid)
			}
		})
	}
}

func TestTypstSourceStaysUnpublished(t *testing.T) {
	const source = "= Example\n```html\n<div>Example</div>\n```"
	if err := validateSource(domain.ContentFormatTypst, source); err != nil {
		t.Errorf("Typst draft rejected: %v", err)
	}
	if err := ValidatePublishableSource(domain.ContentFormatTypst, source); !IsValidationError(err) {
		t.Errorf("publishing Typst returned %v, want validation error", err)
	}
}

func TestValidateAssetCount(t *testing.T) {
	if err := ValidateAssetCount(MaxAssets); err != nil {
		t.Error(err)
	}
	if err := ValidateAssetCount(MaxAssets + 1); !IsValidationError(err) {
		t.Errorf("ValidateAssetCount above limit = %v, want validation error", err)
	}
}

func BenchmarkValidatePublishableSource(b *testing.B) {
	const paragraph = "Текст конспекта с терминами и примером.\n\n"
	for _, n := range []int{4 * 1024, MaxSourceBytes} {
		source := strings.Repeat(paragraph, n/len(paragraph))
		name := "near-limit"
		if n < MaxSourceBytes {
			name = "4-KiB"
		}
		b.Run(name, func(b *testing.B) {
			b.SetBytes(int64(len(source)))
			b.ReportAllocs()
			for b.Loop() {
				if err := ValidatePublishableSource(domain.ContentFormatMarkdown, source); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
