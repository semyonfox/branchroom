// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package secrets

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	system_model "gitea.dev/models/system"
	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/log"
	"gitea.dev/modules/private"
	"gitea.dev/modules/secretscan"
	"gitea.dev/modules/setting"
)

const (
	maxPatchBytes     = 32 * 1024 * 1024
	maxUTF16BlobBytes = 1024 * 1024 // larger binaries are not opened to look for a UTF-16 BOM
	maxFindings       = 5
	pushScanTimeout   = 30 * time.Second
)

// ScanPush checks the files added or changed in commits entering the repository.
func ScanPush(ctx context.Context, repo git.RepositoryFacade, env []string, newCommitIDs ...string) ([]secretscan.Finding, error) {
	if !setting.SecretScanning.PushProtection || len(newCommitIDs) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, pushScanTimeout)
	defer cancel()
	batch, err := git.NewBatchWithEnv(ctx, repo, env)
	if err != nil {
		return nil, err
	}
	defer batch.Close()

	remaining := int64(maxPatchBytes)
	readBlob := func(oid string) ([]byte, error) {
		info, err := batch.QueryInfo(oid)
		if err != nil || info.Type != string(git.ObjectBlob) || info.Size < 2 || info.Size > min(remaining, maxUTF16BlobBytes) {
			return nil, err
		}
		_, reader, err := batch.QueryContent(oid)
		if err != nil {
			return nil, err
		}
		bom, err := reader.Peek(2)
		if err != nil {
			return nil, err
		}
		if !(bom[0] == 0xff && bom[1] == 0xfe || bom[0] == 0xfe && bom[1] == 0xff) {
			_, err = reader.Discard(int(info.Size) + 1)
			return nil, err
		}
		remaining -= info.Size
		data := make([]byte, info.Size)
		if _, err := io.ReadFull(reader, data); err != nil {
			return nil, err
		}
		_, err = reader.Discard(1)
		return data, err
	}

	cmd := gitcmd.NewCommand("log", "--patch", "--full-index", "--no-abbrev", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", "--no-ext-diff", "--no-textconv", "--format=commit:%H").
		AddDynamicArguments(newCommitIDs...).AddArguments("--not", "--all")
	stdout, closeStdout := cmd.MakeStdoutPipe()
	defer closeStdout()

	var findings []secretscan.Finding
	runErr := cmd.WithEnv(env).WithRepo(repo).
		WithPipelineFunc(func(gitCtx gitcmd.Context) error {
			var err error
			findings, err = setting.SecretScanningScanner.PatchWithBlobs(io.LimitReader(stdout, maxPatchBytes), maxFindings, readBlob)
			return gitCtx.CancelPipeline(err)
		}).
		RunWithStderr(ctx)
	if runErr != nil {
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, runErr
		}
		// like the byte cap, the timeout lets the unscanned rest through rather than fail the push
		log.Warn("ScanPush: stopped after %s, the rest of the push was not scanned", pushScanTimeout)
	}
	return findings, nil
}

// PushProtectionMessage describes the matches without returning complete credentials.
func PushProtectionMessage(target string, findings []secretscan.Finding) string {
	var msg strings.Builder
	fmt.Fprintf(&msg, "Push to %s rejected: it adds what look like secrets.\n", target)
	for _, f := range findings {
		fmt.Fprintf(&msg, "  %s in %s, commit %s: %s\n", f.Kind, f.Path, f.Commit[:min(len(f.Commit), 10)], f.Redacted)
	}
	if setting.SecretScanning.AllowBypass {
		msg.WriteString("Remove them from the commits and revoke any that were real. If none is a secret, push again with -o " + private.GitPushOptionAllowSecrets + "=true. The bypass is recorded for the site admins")
	} else {
		msg.WriteString("Remove them from the commits and revoke any that were real. This server does not allow the push protection bypass")
	}
	return msg.String()
}

// RecordPushBypass leaves a site-admin notice containing only redacted matches.
func RecordPushBypass(ctx context.Context, username, repoName, target string, findings []secretscan.Finding) {
	var desc strings.Builder
	fmt.Fprintf(&desc, "%s bypassed secret push protection on %s, %s:", username, repoName, target)
	for _, f := range findings {
		fmt.Fprintf(&desc, " %s in %s (%s), commit %.10s;", f.Kind, f.Path, f.Redacted, f.Commit)
	}
	if err := system_model.CreateNotice(ctx, system_model.NoticeRepository, strings.TrimSuffix(desc.String(), ";")); err != nil {
		log.Error("record secret scan bypass for %s: %v", repoName, err)
	}
}
