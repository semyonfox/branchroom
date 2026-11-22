// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package secrets

import (
	"context"
	"io"

	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/secretscan"
	"gitea.dev/modules/setting"
)

const (
	maxPatchBytes = 32 * 1024 * 1024
	maxFindings   = 5
)

// ScanPush checks the files added or changed in commits entering the repository.
func ScanPush(ctx context.Context, repo git.RepositoryFacade, env []string, newCommitID string) ([]secretscan.Finding, error) {
	if !setting.SecretScanning.PushProtection {
		return nil, nil
	}
	batch, err := git.NewBatchWithEnv(ctx, repo, env)
	if err != nil {
		return nil, err
	}
	defer batch.Close()

	remaining := int64(maxPatchBytes)
	readBlob := func(oid string) ([]byte, error) {
		info, err := batch.QueryInfo(oid)
		if err != nil || info.Type != string(git.ObjectBlob) || info.Size < 2 || info.Size > remaining {
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
		AddDynamicArguments(newCommitID).AddArguments("--not", "--all")
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
		return nil, runErr
	}
	return findings, nil
}
