package validator

import (
	"html"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/overmindv/content/internal/pkg/domain"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

const (
	MaxSourceBytes = 128 * 1024
	MaxAssets      = 32
	maxLinks       = 128
	maxLinkBytes   = 2048
)

var markdownParser = goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser()

func ValidateSourceSize(size int) error {
	if size > MaxSourceBytes {
		return Error{Message: "source exceeds 128 KiB"}
	}

	return nil
}

func ValidateAssetCount(count int) error {
	if count > MaxAssets {
		return Error{Message: "too many attachments (maximum 32)"}
	}

	return nil
}

func ValidatePublishableSource(format domain.ContentFormat, source string) error {
	if format != domain.ContentFormatMarkdown {
		return Error{Message: "only markdown can be published"}
	}

	return validateSource(format, source)
}

func validateSource(format domain.ContentFormat, source string) error {
	if err := ValidateSourceSize(len(source)); err != nil {
		return err
	}
	if !utf8.ValidString(source) || strings.IndexByte(source, 0) >= 0 {
		return Error{Message: "source must be UTF-8 text without NUL"}
	}
	if strings.TrimSpace(source) == "" {
		return Error{Message: "source is required"}
	}
	if format != domain.ContentFormatMarkdown {
		return nil
	}

	input := []byte(source)
	root := markdownParser.Parse(text.NewReader(input))
	links := 0

	return ast.Walk(root, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		var target []byte
		switch node := node.(type) {
		case *ast.RawHTML, *ast.HTMLBlock:
			return ast.WalkStop, Error{Message: "raw HTML is not allowed"}
		case *ast.Image:
			return ast.WalkStop, Error{Message: "inline images are not allowed"}
		case *ast.Link:
			target = node.Destination
		case *ast.AutoLink:
			target = node.URL(input)
		default:
			return ast.WalkContinue, nil
		}

		links++
		if links > maxLinks {
			return ast.WalkStop, Error{Message: "too many links (maximum 128)"}
		}
		if len(target) > maxLinkBytes || !isAllowedLink(html.UnescapeString(string(target))) {
			return ast.WalkStop, Error{Message: "links must be HTTPS URLs or anchors up to 2048 bytes"}
		}

		return ast.WalkContinue, nil
	})
}

func isAllowedLink(target string) bool {
	if target == "" || strings.ContainsAny(target, `\<>`) || strings.ContainsFunc(target, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) {
		return false
	}
	if strings.HasPrefix(target, "#") {
		return len(target) > 1
	}
	u, err := url.Parse(target)

	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}
