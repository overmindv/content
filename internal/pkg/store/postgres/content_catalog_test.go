package postgres

import (
	"strings"
	"testing"

	"github.com/overmindv/content/internal/pkg/domain"
)

func TestCatalogQueryFiltersTitleCaseInsensitively(t *testing.T) {
	query, args := catalogQuery(domain.CatalogFilter{ActorID: "actor", Search: "Линейная", PageSize: 5}, nil)

	if !strings.Contains(query, "i.title ILIKE") {
		t.Fatalf("catalog search must be case-insensitive, query: %s", query)
	}
	if len(args) != 3 || args[1] != "%Линейная%" {
		t.Fatalf("catalog search argument = %#v, want wrapped title filter", args)
	}
}
