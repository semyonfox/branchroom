// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/log"
	"gitea.dev/modules/secretscan"
	"gitea.dev/modules/setting"
)

const (
	gitPushOptionAllowSecrets = "secret_scanning.allow"
	// a push this large is history being imported, not a credential slipping in, so the rest goes unscanned
	secretScanMaxPatchBytes = 32 * 1024 * 1024
	secretScanMaxFindings   = 5
)

// assertNoPushedSecrets rejects the push when the commits it adds introduce a recognisable credential
func assertNoPushedSecrets(ctx *preReceiveContext, newCommitID, branchName string) bool {
	if !setting.SecretScanning.PushProtection || ctx.opts.GitPushOptions.Bool(gitPushOptionAllowSecrets).Value() {
		return true
	}

	// before the refs move, "--not --all" leaves exactly the commits this push adds
	cmd := gitcmd.NewCommand("log", "-p", "--no-color", "--no-ext-diff", "--no-textconv", "--format=commit:%H").
		AddDynamicArguments(newCommitID).AddArguments("--not", "--all")
	stdout, stdoutClose := cmd.MakeStdoutPipe()
	defer stdoutClose()

	var findings []secretscan.Finding
	err := cmd.WithEnv(ctx.env).WithRepo(ctx.Repo.Repository).
		WithPipelineFunc(func(gitCtx gitcmd.Context) error {
			var err error
			findings, err = secretscan.Patch(io.LimitReader(stdout, secretScanMaxPatchBytes), secretScanMaxFindings)
			if err != nil {
				return gitCtx.CancelPipeline(err)
			}
			// stops git early when the scan ended before the log did
			return gitCtx.CancelPipeline(nil)
		}).
		RunWithStderr(ctx)
	if err != nil {
		log.Error("secret scan of %s in %s: %v", newCommitID, ctx.Repo.Repository.FullName(), err)
		ctx.PrivateInternalErrorf("Unable to scan the pushed commits for secrets")
		return false
	}
	if len(findings) == 0 {
		return true
	}

	var msg strings.Builder
	fmt.Fprintf(&msg, "Push to %s rejected: it adds what look like secrets.\n", branchName)
	for _, f := range findings {
		fmt.Fprintf(&msg, "  %s in %s, commit %s: %s\n", f.Kind, f.Path, f.Commit[:min(len(f.Commit), 10)], f.Redacted)
	}
	msg.WriteString("Remove them from the commits and revoke any that were real. If none is a secret, push again with -o " + gitPushOptionAllowSecrets + "=true")
	ctx.PrivateUserErrorf(http.StatusForbidden, "%s", msg.String())
	return false
}
