// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"net/http"

	"gitea.dev/modules/log"
	"gitea.dev/modules/private"
	"gitea.dev/modules/setting"
	secret_service "gitea.dev/services/secrets"
)

// assertNoPushedSecrets rejects the push when the commits it adds introduce a recognisable credential
func assertNoPushedSecrets(ctx *preReceiveContext, newCommitID, branchName string) bool {
	if !setting.SecretScanning.PushProtection {
		return true
	}
	// the scan still runs on a bypass, so only a push that really skips a match
	// is recorded
	bypass := ctx.opts.GitPushOptions.Bool(private.GitPushOptionAllowSecrets).Value()

	findings, err := secret_service.ScanPush(ctx, ctx.Repo.Repository, ctx.env, newCommitID)
	if err != nil {
		log.Error("secret scan of %s in %s: %v", newCommitID, ctx.Repo.Repository.FullName(), err)
		ctx.PrivateInternalErrorf("Unable to scan the pushed commits for secrets")
		return false
	}
	if len(findings) == 0 {
		return true
	}
	if bypass && setting.SecretScanning.AllowBypass {
		secret_service.RecordPushBypass(ctx, ctx.opts.UserName, ctx.Repo.Repository.FullName(), "branch "+branchName, findings)
		return true
	}

	ctx.PrivateUserErrorf(http.StatusForbidden, "%s", secret_service.PushProtectionMessage(branchName, findings))
	return false
}
