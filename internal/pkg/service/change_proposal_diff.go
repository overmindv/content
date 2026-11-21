package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sergi/go-diff/diffmatchpatch"
)

const (
	proposalDiffSourceTimeout = time.Second
	proposalDiffBudget        = 50 * time.Millisecond
	maxProposalDiffBytes      = 32 * 1024
	maxProposalDiffLines      = 8192
	diffContextLines          = 3
)

type diffLine struct {
	text                 string
	oldBefore, newBefore int
	mark                 byte
}

func makeUnifiedDiff(ctx context.Context, base, target string, budget time.Duration) (string, bool, error) {
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if base == target {
		return "", false, nil
	}
	if countLines(base)+countLines(target) > maxProposalDiffLines {
		return "", true, nil
	}
	deadline := time.Now().Add(budget)
	if clientDeadline, ok := ctx.Deadline(); ok && clientDeadline.Before(deadline) {
		deadline = clientDeadline
	}
	dmp := diffmatchpatch.New()
	old, next, table := dmp.DiffLinesToRunes(base, target)
	dmp.DiffTimeout = time.Until(deadline)
	if dmp.DiffTimeout <= 0 {
		return "", true, ctx.Err()
	}
	edits := dmp.DiffMainRunes(old, next, false)
	if err := ctx.Err(); err != nil {
		return "", false, err
	}
	if !time.Now().Before(deadline) {
		return "", true, nil
	}
	lines := expandDiffLines(dmp.DiffCharsToLines(edits, table), len(old)+len(next))
	var output strings.Builder
	start, end := -1, -1
	for i, line := range lines {
		if line.mark == ' ' {
			continue
		}
		from, to := max(0, i-diffContextLines), min(len(lines), i+diffContextLines+1)
		if start == -1 {
			start, end = from, to

			continue
		}
		if from > end {
			if !time.Now().Before(deadline) || !writeDiffHunk(&output, lines[start:end]) {
				return output.String(), true, ctx.Err()
			}
			start = from
		}
		end = to
	}
	if start != -1 && (!time.Now().Before(deadline) || !writeDiffHunk(&output, lines[start:end])) {
		return output.String(), true, ctx.Err()
	}

	return output.String(), false, ctx.Err()
}

func countLines(source string) int {
	n := strings.Count(source, "\n")
	if source != "" && !strings.HasSuffix(source, "\n") {
		n++
	}

	return n
}

func expandDiffLines(edits []diffmatchpatch.Diff, capacity int) []diffLine {
	lines := make([]diffLine, 0, capacity)
	old, next := 0, 0
	for _, edit := range edits {
		mark := byte(' ')
		switch edit.Type {
		case diffmatchpatch.DiffDelete:
			mark = '-'
		case diffmatchpatch.DiffInsert:
			mark = '+'
		}
		for line := range strings.SplitAfterSeq(edit.Text, "\n") {
			if line == "" {
				continue
			}
			lines = append(lines, diffLine{text: line, oldBefore: old, newBefore: next, mark: mark})
			if mark != '+' {
				old++
			}
			if mark != '-' {
				next++
			}
		}
	}

	return lines
}

func writeDiffHunk(output *strings.Builder, lines []diffLine) bool {
	const (
		fileHeader = "--- base\n+++ revision\n"
		noNewline  = "\n\\ No newline at end of file\n"
	)
	old, next, size := 0, 0, 0
	for _, line := range lines {
		if line.mark != '+' {
			old++
		}
		if line.mark != '-' {
			next++
		}
		size += 1 + len(line.text)
		if !strings.HasSuffix(line.text, "\n") {
			size += len(noNewline)
		}
	}
	oldStart, nextStart := lines[0].oldBefore, lines[0].newBefore
	if old > 0 {
		oldStart++
	}
	if next > 0 {
		nextStart++
	}
	header := fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, old, nextStart, next)
	if output.Len() == 0 {
		size += len(fileHeader)
	}
	if output.Len()+len(header)+size > maxProposalDiffBytes {
		return false
	}
	if output.Len() == 0 {
		output.WriteString(fileHeader)
	}
	output.WriteString(header)
	for _, line := range lines {
		output.WriteByte(line.mark)
		output.WriteString(line.text)
		if !strings.HasSuffix(line.text, "\n") {
			output.WriteString(noNewline)
		}
	}

	return true
}
