// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package markdown

import (
	"fmt"
	"strings"

	"gitea.dev/modules/markup"
	giteautil "gitea.dev/modules/util"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

const reviewSuggestionAttr = "review-suggestion"

// Suggestions returns the suggestion fences of a comment, LF-normalised and in document order.
// it parses exactly as rendering does, so what gets applied is what the page shows
func Suggestions(rctx *markup.RenderContext, content string) []string {
	source := giteautil.NormalizeEOL([]byte(content))
	source, _ = ExtractMetadataBytes(source, &RenderConfig{})
	pc := newParserContext(rctx)
	pc.Set(renderConfigKey, &RenderConfig{})
	doc := SpecializedMarkdown(rctx).Parser().Parse(text.NewReader(source), parser.WithContext(pc))
	var suggestions []string
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if _, marked := n.AttributeString(reviewSuggestionAttr); entering && marked {
			suggestions = append(suggestions, string(n.Lines().Value(source)))
		}
		return ast.WalkContinue, nil
	})
	return suggestions
}

// SuggestionContext reads the commented lines (count of them, ending at line) and up to 4
// lines above them on the same side from the saved unified diff, oldest first
func SuggestionContext(patch string, line, count int64) ([]string, bool) {
	var oldLine, newLine int64
	var seen []string
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
			seen = seen[:0]
			continue
		}
		if !inHunk || s == "" {
			continue
		}
		switch s[0] {
		case ' ', '+', '-':
			onSide := (line > 0 && s[0] != '-') || (line < 0 && s[0] != '+')
			if onSide {
				seen = append(seen, strings.TrimSuffix(s[1:], "\r"))
				if (line > 0 && newLine == line) || (line < 0 && oldLine == -line) {
					if int64(len(seen)) < count {
						return nil, false
					}
					return seen[max(0, len(seen)-int(count)-4):], true
				}
			}
			if s[0] != '+' {
				oldLine++
			}
			if s[0] != '-' {
				newLine++
			}
		}
	}
	return nil, false
}

// SuggestionOriginal reads the commented lines from the saved unified diff. startLine is 0
// for a single-line comment and otherwise unsigned, line is negative on the old side
func SuggestionOriginal(patch string, startLine, line int64) ([]string, bool) {
	count := SuggestionLineCount(startLine, line)
	if count < 1 {
		return nil, false
	}
	lines, ok := SuggestionContext(patch, line, count)
	if !ok {
		return nil, false
	}
	return lines[len(lines)-int(count):], true
}

// SuggestionLineCount returns how many lines a comment covers, or 0 for an invalid range
func SuggestionLineCount(startLine, line int64) int64 {
	if startLine == 0 {
		return 1
	}
	end := max(line, -line)
	if startLine < 1 || startLine > end {
		return 0
	}
	return end - startLine + 1
}
