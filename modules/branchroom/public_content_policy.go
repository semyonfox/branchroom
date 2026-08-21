// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bytes"
	"errors"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	maxPublicContentBytes     = 1 << 20
	maxPublicContentPathBytes = 4096
)

var (
	// ErrInvalidPublicContentPath never echoes an untrusted repository path.
	ErrInvalidPublicContentPath = errors.New("invalid Branchroom public content path")
	// ErrPublicContentTooLarge tells a future streaming caller not to silently skip content.
	ErrPublicContentTooLarge = errors.New("Branchroom public content is too large to scan")
)

// PublicContentRule identifies a content policy violation without retaining a matched value.
type PublicContentRule string

const (
	PublicContentRuleReservedPath                PublicContentRule = "reserved_path"
	PublicContentRuleInvalidEnvironmentReference PublicContentRule = "invalid_environment_reference"
	PublicContentRuleReferencePathMismatch       PublicContentRule = "environment_reference_path_mismatch"
	PublicContentRulePrivateKey                  PublicContentRule = "private_key"
	PublicContentRuleAWSAccessKey                PublicContentRule = "aws_access_key"
	PublicContentRuleGitHubToken                 PublicContentRule = "github_token"
	PublicContentRuleDefaultGitLabToken          PublicContentRule = "default_gitlab_token"
)

// PublicContentFinding reports a rule and line number only. It deliberately contains no matched bytes.
type PublicContentFinding struct {
	Rule PublicContentRule
	Line int
}

type publicContentDetector struct {
	rule       PublicContentRule
	expression *regexp.Regexp
}

var publicContentDetectors = []publicContentDetector{
	{PublicContentRulePrivateKey, regexp.MustCompile(`-----BEGIN(?: [A-Z0-9]+)? PRIVATE KEY-----`)},
	{PublicContentRuleAWSAccessKey, regexp.MustCompile(`(?:AKIA|ASIA)[A-Z0-9]{16}`)},
	{PublicContentRuleGitHubToken, regexp.MustCompile(`(?:gh[pour]_[A-Za-z0-9]{36}|ghs_[A-Za-z0-9._-]{36,}|github_pat_[A-Za-z0-9_]{20,255})`)},
	{PublicContentRuleDefaultGitLabToken, regexp.MustCompile(`glpat-[A-Za-z0-9_-]{20,}`)},
}

// CheckPublicContent performs conservative, side-effect-free public-content preflight checks.
// It returns at most one finding per rule in fixed rule order, has no source-controlled
// bypass, and is not a substitute for secret-manager controls.
func CheckPublicContent(filePath string, content []byte) ([]PublicContentFinding, error) {
	if err := validatePublicContentPath(filePath); err != nil {
		return nil, err
	}
	if len(content) > maxPublicContentBytes {
		return nil, ErrPublicContentTooLarge
	}
	if finding := checkReservedPublicContentPath(filePath, content); finding != nil {
		return []PublicContentFinding{*finding}, nil
	}

	var findings []PublicContentFinding
	for _, detector := range publicContentDetectors {
		match := detector.expression.FindIndex(content)
		if match == nil {
			continue
		}
		findings = append(findings, PublicContentFinding{
			Rule: detector.rule,
			Line: bytes.Count(content[:match[0]], []byte{'\n'}) + 1,
		})
	}
	return findings, nil
}

func validatePublicContentPath(filePath string) error {
	if filePath == "" || len(filePath) > maxPublicContentPathBytes || !utf8.ValidString(filePath) || filePath != strings.TrimSpace(filePath) || path.IsAbs(filePath) || path.Clean(filePath) != filePath || strings.ContainsAny(filePath, "\\:\x00") || strings.ContainsFunc(filePath, unicode.IsControl) {
		return ErrInvalidPublicContentPath
	}
	for segment := range strings.SplitSeq(filePath, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
			return ErrInvalidPublicContentPath
		}
	}
	return nil
}

func checkReservedPublicContentPath(filePath string, content []byte) *PublicContentFinding {
	rootSegment, remainder, hasRemainder := strings.Cut(filePath, "/")
	if !strings.EqualFold(rootSegment, ".branchroom") {
		return nil
	}
	if rootSegment != ".branchroom" || !hasRemainder {
		return &PublicContentFinding{Rule: PublicContentRuleReservedPath}
	}
	environmentFile, ok := strings.CutPrefix(remainder, "env/")
	if !ok || strings.ContainsRune(environmentFile, '/') || !strings.HasSuffix(environmentFile, ".toml") {
		return &PublicContentFinding{Rule: PublicContentRuleReservedPath}
	}
	environment := strings.TrimSuffix(environmentFile, ".toml")
	if !isEnvironmentReferenceName(environment) {
		return &PublicContentFinding{Rule: PublicContentRuleReservedPath}
	}
	reference, err := ParseEnvironmentReference(content)
	if err != nil {
		return &PublicContentFinding{Rule: PublicContentRuleInvalidEnvironmentReference}
	}
	if reference.Environment != environment {
		return &PublicContentFinding{Rule: PublicContentRuleReferencePathMismatch}
	}
	return nil
}
