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
	"strconv"
	"strings"

	"golang.org/x/text/encoding/unicode"
)

type pattern struct {
	name string
	re   *regexp.Regexp
}

var patterns = []pattern{
	{"Branchroom access token", regexp.MustCompile(`\bbrpat_[0-9a-f]{40}\b`)},
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
	return s.PatchWithBlobs(r, limit, nil)
}

// PatchWithBlobs also scans UTF-16 files identified by Git's --raw --no-abbrev output.
// readBlob returns the bytes of a BOM-marked UTF-16 blob, or nil for other blobs.
func (s *Scanner) PatchWithBlobs(r io.Reader, limit int, readBlob func(string) ([]byte, error)) ([]Finding, error) {
	var findings []Finding
	var commit, path string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "commit:"):
			commit = strings.TrimPrefix(line, "commit:")
		case readBlob != nil && strings.HasPrefix(line, ":"):
			added, err := s.scanUTF16Change(line, commit, limit-len(findings), readBlob)
			if err != nil {
				return nil, err
			}
			findings = append(findings, added...)
			if len(findings) >= limit {
				return findings, nil
			}
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

func scanBlobUTF16(blob []byte) string {
	if len(blob) == 0 {
		return ""
	}
	text, err := unicode.UTF16(unicode.LittleEndian, unicode.ExpectBOM).NewDecoder().Bytes(blob)
	if err != nil {
		return ""
	}
	return string(text)
}

func (s *Scanner) scanUTF16Change(raw, commit string, limit int, readBlob func(string) ([]byte, error)) ([]Finding, error) {
	metadata, paths, _ := strings.Cut(raw, "\t")
	fields := strings.Fields(metadata)
	if len(fields) != 5 || !strings.HasPrefix(fields[1], "100") || fields[2] == fields[3] {
		return nil, nil
	}
	after, err := readBlob(fields[3])
	if err != nil || len(after) == 0 {
		return nil, err
	}
	previous := make(map[string]int)
	if strings.Trim(fields[2], "0") != "" && strings.HasPrefix(fields[0], ":100") {
		before, err := readBlob(fields[2])
		if err != nil {
			return nil, err
		}
		for line := range strings.SplitSeq(scanBlobUTF16(before), "\n") {
			line = strings.TrimSuffix(line, "\r")
			if _, match := s.Line(line); match != "" {
				previous[line]++
			}
		}
	}

	path := paths[strings.LastIndexByte(paths, '\t')+1:]
	if strings.HasPrefix(path, "\"") {
		if decoded, err := strconv.Unquote(path); err == nil {
			path = decoded
		}
	}
	var findings []Finding
	for line := range strings.SplitSeq(scanBlobUTF16(after), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if previous[line] > 0 {
			previous[line]--
			continue
		}
		if kind, match := s.Line(line); kind != "" {
			findings = append(findings, Finding{Commit: commit, Path: path, Kind: kind, Redacted: redact(match)})
			if len(findings) >= limit {
				break
			}
		}
	}
	return findings, nil
}
