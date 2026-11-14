// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package secretscan finds credentials with a recognisable format, the kind push protection can block
// without false positives. Generic high-entropy strings are deliberately not matched
package secretscan

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
)

type pattern struct {
	name string
	re   *regexp.Regexp
}

var patterns = []pattern{
	{"AWS access key", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"GitHub token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{36,}\b`)},
	{"GitHub token", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{60,}\b`)},
	{"GitLab token", regexp.MustCompile(`\bglpat-[A-Za-z0-9_-]{20,}\b`)},
	{"Slack token", regexp.MustCompile(`\bxox[abposr]-[A-Za-z0-9-]{10,}\b`)},
	{"Stripe live key", regexp.MustCompile(`\b[rs]k_live_[A-Za-z0-9]{20,}\b`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"OpenAI API key", regexp.MustCompile(`\bsk-(?:proj|svcacct|admin)-[A-Za-z0-9_-]{20,}`)},
	{"Anthropic API key", regexp.MustCompile(`\bsk-ant-(?:api|admin)\d{2}-[A-Za-z0-9_-]{20,}`)},
	{"npm token", regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b`)},
	{"private key", regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED |PGP )?PRIVATE KEY(?: BLOCK)?-----`)},
}

// Scanner adds site-defined patterns to the built-in vendor patterns.
// The zero value uses only the built-in patterns.
type Scanner struct {
	custom []pattern
}

// NewScanner compiles all patterns before returning a scanner.
func NewScanner(custom map[string]string) (*Scanner, error) {
	names := make([]string, 0, len(custom))
	for name := range custom {
		names = append(names, name)
	}
	slices.Sort(names)
	scanner := &Scanner{}
	for _, name := range names {
		re, err := regexp.Compile(custom[name])
		if err != nil {
			return nil, fmt.Errorf("NewScanner: invalid pattern %q", name)
		}
		if re.MatchString("") {
			return nil, fmt.Errorf("NewScanner: pattern %q matches empty text", name)
		}
		scanner.custom = append(scanner.custom, pattern{name: name, re: re})
	}
	return scanner, nil
}

// documentation examples, such as AWS's AKIAIOSFODNN7EXAMPLE, are not secrets
var placeholder = regexp.MustCompile(`(?i)example|x{8,}|0{16,}`)

// Finding is one credential added by a commit
type Finding struct {
	Commit string
	Path   string
	Kind   string
	// Redacted keeps only the first characters of the match, safe to print back to the pusher
	Redacted string
}

// Line returns what the line looks like it holds, or "" when nothing
func Line(line string) (kind, match string) {
	return new(Scanner).Line(line)
}

// Line checks built-in and site-defined patterns using the same exclusions.
func (s *Scanner) Line(line string) (kind, match string) {
	for _, group := range [][]pattern{patterns, s.custom} {
		for _, p := range group {
			if m := p.re.FindString(line); m != "" && !placeholder.MatchString(m) {
				return p.name, m
			}
		}
	}
	return "", ""
}

func redact(s string) string {
	const keep = 8
	if len(s) <= keep {
		return strings.Repeat("*", len(s))
	}
	return s[:keep] + strings.Repeat("*", 8)
}

// Patch scans `git log -p --format=commit:%H` output and returns at most limit findings,
// checking only the lines the commits add
func Patch(r io.Reader, limit int) ([]Finding, error) {
	return new(Scanner).Patch(r, limit)
}

// Patch scans added lines with the scanner's built-in and custom patterns.
func (s *Scanner) Patch(r io.Reader, limit int) ([]Finding, error) {
	var findings []Finding
	var commit, path string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "commit:"):
			commit = strings.TrimPrefix(line, "commit:")
		case strings.HasPrefix(line, "+++ "):
			path = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
		case strings.HasPrefix(line, "+"):
			if kind, match := s.Line(line[1:]); kind != "" {
				findings = append(findings, Finding{Commit: commit, Path: path, Kind: kind, Redacted: redact(match)})
				if len(findings) >= limit {
					return findings, nil
				}
			}
		}
	}
	return findings, scanner.Err()
}
