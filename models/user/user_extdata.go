// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"errors"
	"slices"
	"strconv"
	"strings"

	"gitea.dev/models/auth"
	"gitea.dev/modules/json"
	"gitea.dev/modules/timeutil"
)

type ExtDoerData interface {
	EncodeToString() string
	DecodeFromString(string) error
}

type extDoerGiteaActions struct {
	TaskID int64
}

var _ ExtDoerData = (*extDoerGiteaActions)(nil)

func (e *extDoerGiteaActions) EncodeToString() string {
	return "gitea-actions:" + strconv.FormatInt(e.TaskID, 10)
}

func (e *extDoerGiteaActions) DecodeFromString(s string) (err error) {
	idStr, _ := strings.CutPrefix(s, "gitea-actions:")
	e.TaskID, err = strconv.ParseInt(idStr, 10, 64)
	return err
}

type extDoerDeployKey struct {
	DeployKeyID int64
}

var _ ExtDoerData = (*extDoerDeployKey)(nil)

func (e *extDoerDeployKey) EncodeToString() string {
	return "deploy-key:" + strconv.FormatInt(e.DeployKeyID, 10)
}

func (e *extDoerDeployKey) DecodeFromString(s string) (err error) {
	idStr, _ := strings.CutPrefix(s, "deploy-key:")
	e.DeployKeyID, err = strconv.ParseInt(idStr, 10, 64)
	return err
}

const (
	CredentialAccessToken = "access-token"
	CredentialOAuth2Grant = "oauth2-grant"
)

type extDoerToken struct {
	repositoryIDs []int64
	expiresUnix   timeutil.TimeStamp
	credential    string // empty for package registry tokens
	scope         auth.AccessTokenScope
	publicOnly    bool
}

var _ ExtDoerData = (*extDoerToken)(nil)

func NewTokenExtDoerData(credentialKind string, credentialID int64, scope auth.AccessTokenScope) ExtDoerData {
	publicOnly, err := scope.PublicOnly()
	e := &extDoerToken{scope: scope, publicOnly: publicOnly || err != nil}
	if credentialKind != "" {
		e.credential = credentialKind + ":" + strconv.FormatInt(credentialID, 10)
	}
	return e
}

func (e *extDoerToken) EncodeToString() string {
	s := e.credential + "|" + string(e.scope)
	if len(e.repositoryIDs) > 0 || e.expiresUnix != 0 {
		data, _ := json.Marshal(tokenRestrictions{e.repositoryIDs, e.expiresUnix})
		s += "|" + string(data)
	}
	return s
}

func (e *extDoerToken) DecodeFromString(s string) error {
	credential, scope, ok := strings.Cut(s, "|")
	if !ok {
		return errors.New("invalid token doer data")
	}
	scope, restrictions, _ := strings.Cut(scope, "|")
	if restrictions != "" {
		var r tokenRestrictions
		if err := json.Unmarshal([]byte(restrictions), &r); err != nil {
			return err
		}
		e.repositoryIDs, e.expiresUnix = r.RepositoryIDs, r.ExpiresUnix
	}
	publicOnly, err := auth.AccessTokenScope(scope).PublicOnly()
	if err != nil {
		return err
	}
	e.credential, e.scope, e.publicOnly = credential, auth.AccessTokenScope(scope), publicOnly
	return nil
}

func getDoerToken(u *User) *extDoerToken {
	if u == nil {
		return nil
	}
	token, _ := u.ExtDoerData.(*extDoerToken)
	return token
}

func GetDoerTokenScope(u *User) (auth.AccessTokenScope, bool) {
	if token := getDoerToken(u); token != nil {
		return token.scope, true
	}
	return "", false
}

func IsPublicOnlyDoer(u *User) bool {
	token := getDoerToken(u)
	return token != nil && token.publicOnly
}

func DoerCanViewPrivate(doer *User) bool {
	return doer != nil && !IsPublicOnlyDoer(doer)
}

func DoerIsSiteAdminFullAccess(doer *User) bool {
	if !DoerCanViewPrivate(doer) || !doer.IsAdmin || len(GetDoerRepositoryIDs(doer)) > 0 {
		return false
	}
	scope, hasScope := GetDoerTokenScope(doer)
	return !hasScope || scope == auth.AccessTokenScopeAll
}

func DoerTokenAllowsOwner(doer, owner *User) bool {
	return !IsPublicOnlyDoer(doer) || owner.Visibility.IsPublic()
}

func GetDoerCredential(u *User) string {
	if u == nil || u.ExtDoerData == nil {
		return ""
	}
	if token := getDoerToken(u); token != nil {
		return token.credential
	}
	return u.ExtDoerData.EncodeToString()
}

type tokenRestrictions struct {
	RepositoryIDs []int64
	ExpiresUnix   timeutil.TimeStamp
}

func NewAccessTokenExtDoerData(t *auth.AccessToken) ExtDoerData {
	publicOnly, err := t.Scope.PublicOnly()
	return &extDoerToken{credential: CredentialAccessToken + ":" + strconv.FormatInt(t.ID, 10), scope: t.Scope, publicOnly: publicOnly || err != nil, repositoryIDs: t.RepositoryIDs, expiresUnix: t.ExpiresUnix}
}

func GetDoerRepositoryIDs(u *User) []int64 {
	if t := getDoerToken(u); t != nil {
		return t.repositoryIDs
	}
	return nil
}

func DoerTokenAllowsRepositoryID(u *User, id int64) bool {
	if t := getDoerToken(u); t != nil {
		if t.expiresUnix != 0 && t.expiresUnix <= timeutil.TimeStampNow() {
			return false
		}
		return len(t.repositoryIDs) == 0 || slices.Contains(t.repositoryIDs, id)
	}
	return true
}
