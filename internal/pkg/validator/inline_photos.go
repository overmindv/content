package validator

import (
	"bytes"
	"slices"

	"github.com/google/uuid"
	"github.com/yuin/goldmark/ast"
)

const (
	PhotoBlockLanguage = "ovm-photos"
	maxPhotosPerBlock  = 10
)

type photoReferences struct {
	ids   []string
	count int
}

func (p *photoReferences) addBlock(block *ast.FencedCodeBlock, source []byte) error {
	lines := block.Lines()
	if lines.Len() == 0 || lines.Len() > maxPhotosPerBlock {
		return Error{Message: "ovm-photos must contain 1 to 10 attachment IDs"}
	}
	p.count += lines.Len()
	if p.count > MaxAssets {
		return Error{Message: "too many inline photo references (maximum 32)"}
	}

	var group [maxPhotosPerBlock]string
	for i := range lines.Len() {
		segment := lines.At(i)
		raw := bytes.Trim(segment.Value(source), " \t\r\n")
		if len(raw) != 36 {
			return Error{Message: "ovm-photos entries must be canonical attachment UUIDs"}
		}
		id, err := uuid.ParseBytes(raw)
		value := string(raw)
		if err != nil || id == uuid.Nil || value != id.String() {
			return Error{Message: "ovm-photos entries must be canonical attachment UUIDs"}
		}
		if slices.Contains(group[:i], value) {
			return Error{Message: "duplicate photo in ovm-photos block"}
		}
		group[i] = value
		if !slices.Contains(p.ids, value) {
			p.ids = append(p.ids, value)
		}
	}

	return nil
}
