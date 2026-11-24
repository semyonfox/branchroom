// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package markdown

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Suggestions returns suggestion fences, in document order.
func Suggestions(content string) []string {
	source := []byte(content)
	doc := goldmark.DefaultParser().Parse(text.NewReader(source))
	var suggestions []string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if fence, ok := n.(*ast.FencedCodeBlock); entering && ok && fence.Info != nil && string(fence.Info.Segment.Value(source)) == "suggestion" {
			suggestions = append(suggestions, string(fence.Lines().Value(source)))
		}
		return ast.WalkContinue, nil
	})
	return suggestions
}

// SuggestionOriginal reads the commented line from its saved unified diff.
func SuggestionOriginal(patch string, line int64) (string, bool) {
	var oldLine, newLine int64
	inHunk := false
	for s := range strings.SplitSeq(patch, "\n") {
		if strings.HasPrefix(s, "@@ ") {
			fields := strings.Fields(s)
			if len(fields) < 4 {
				continue
			}
			_, e1 := fmt.Sscanf(strings.Split(fields[1], ",")[0], "-%d", &oldLine)
			_, e2 := fmt.Sscanf(strings.Split(fields[2], ",")[0], "+%d", &newLine)
			inHunk = e1 == nil && e2 == nil
			continue
		}
		if !inHunk || s == "" {
			continue
		}
		switch s[0] {
		case ' ', '+', '-':
			if (line > 0 && s[0] != '-' && newLine == line) || (line < 0 && s[0] != '+' && oldLine == -line) {
				return s[1:], true
			}
			if s[0] != '+' {
				oldLine++
			}
			if s[0] != '-' {
				newLine++
			}
		}
	}
	return "", false
}

func SuggestionOriginalRange(patch string, startLine, line int64) (string, bool) {
	if startLine == 0 {
		return SuggestionOriginal(patch, line)
	}
	end := line
	if end < 0 {
		end = -end
	}
	if startLine < 1 || startLine > end {
		return "", false
	}
	var lines []string
	for n := startLine; n <= end; n++ {
		signed := n
		if line < 0 {
			signed = -n
		}
		original, ok := SuggestionOriginal(patch, signed)
		if !ok {
			return "", false
		}
		lines = append(lines, original)
	}
	return strings.Join(lines, "\n"), true
}
