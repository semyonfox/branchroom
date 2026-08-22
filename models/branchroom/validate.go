// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"

	"gitea.dev/models/db"
	"gitea.dev/modules/setting"

	"github.com/google/uuid"
)

const (
	maxScopeNameLength  = 255
	maxConditionLength  = 255
	maxResourceIDLength = 255
	maxRequestIDLength  = 64
	maxReasonCodeLength = 64
	sha256HexLength     = 64
)

// IsCanonicalUUID reports whether value is a canonical lowercase hyphenated UUID string.
func IsCanonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func isScopeName(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > maxScopeNameLength {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}

func isSHA256Hex(value string) bool {
	if len(value) != sha256HexLength || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// IsGitObjectID reports whether value is a canonical SHA-1 or SHA-256 Git object ID.
func IsGitObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// IsConditionValue reports whether value can safely be stored as a workload condition.
func IsConditionValue(value string) bool {
	if len(value) > maxConditionLength || value != strings.TrimSpace(value) {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}

func isActorIDValid(actorType ActorType, actorID int64) bool {
	if actorType == ActorTypeAnonymous || actorType == ActorTypeSystem {
		return actorID == 0
	}
	return actorID > 0
}

func isOptionalSafeLogicalID(value string, maxLength int) bool {
	return value == "" || isSafeLogicalID(value, maxLength)
}

func isSafeLogicalID(value string, maxLength int) bool {
	if value == "" || len(value) > maxLength || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._-/:", r) {
			return false
		}
	}
	return true
}

func isReasonCode(value string) bool {
	if len(value) == 0 || len(value) > maxReasonCodeLength {
		return false
	}
	for index, r := range value {
		if index == 0 {
			if r < 'a' || r > 'z' {
				return false
			}
			continue
		}
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func branchroomSupportsForUpdate() bool {
	return setting.Database.Type.IsMySQL() || setting.Database.Type.IsPostgreSQL()
}

func init() {
	db.RegisterModel(new(Scope))
	db.RegisterModel(new(Grant))
	db.RegisterModel(new(Decision))
	db.RegisterModel(new(AuditEvent))
}
