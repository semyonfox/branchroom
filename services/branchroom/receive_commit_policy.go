// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"strings"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/private"
)

const receiveGitVerificationTimeout = 10 * time.Second

// verifyReceiveCommitUpdates checks the objects exposed to the pre-receive hook
// through Git's quarantine environment. It accepts only new commits that extend
// the workspace base and existing protected refs without force updates.
func verifyReceiveCommitUpdates(ctx context.Context, workspace *branchroom_model.Workspace, updates []branchroom_module.ReceiveRefUpdate, quarantine branchroom_module.ReceiveQuarantineValues) bool {
	if workspace == nil {
		return false
	}
	objectFormat := git.ObjectFormatFromName(workspace.ObjectFormatName)
	if !git.IsValidObjectFormat(workspace.ObjectFormatName) || !git.IsStringValidObjectID(objectFormat, workspace.PublicBaseSHA) {
		return false
	}
	restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
	if err != nil {
		return false
	}
	environ := receiveQuarantineEnv(quarantine)
	verifyCtx, cancel := context.WithTimeout(ctx, receiveGitVerificationTimeout)
	defer cancel()
	if !receiveRepositoryUsesObjectFormat(verifyCtx, restricted, environ, objectFormat) {
		return false
	}

	seenRefs := make(map[string]struct{}, len(updates))
	for _, update := range updates {
		if _, exists := seenRefs[update.Ref]; exists ||
			!git.IsStringValidObjectID(objectFormat, update.OldOID) ||
			!git.IsStringValidObjectID(objectFormat, update.NewOID) {
			return false
		}
		seenRefs[update.Ref] = struct{}{}
	}
	for _, update := range updates {
		if !receiveObjectIsCommit(verifyCtx, restricted, environ, update.NewOID) ||
			!receiveAncestor(verifyCtx, restricted, environ, workspace.PublicBaseSHA, update.NewOID) {
			return false
		}
		currentOID, exists, ok := receiveCurrentRef(verifyCtx, restricted, environ, update.Ref, objectFormat)
		if !ok {
			return false
		}
		if update.OldOID == objectFormat.EmptyObjectID().String() {
			if exists {
				return false
			}
			continue
		}
		if !exists || currentOID != update.OldOID || !receiveObjectIsCommit(verifyCtx, restricted, environ, update.OldOID) ||
			!receiveAncestor(verifyCtx, restricted, environ, update.OldOID, update.NewOID) {
			return false
		}
	}
	return true
}

func receiveQuarantineEnv(quarantine branchroom_module.ReceiveQuarantineValues) []string {
	return append(gitcmd.CommonGitCmdEnvs(),
		private.GitObjectDirectory+"="+quarantine.ObjectDirectory,
		private.GitAlternativeObjectDirectories+"="+quarantine.AlternateObjectDirectories,
		private.GitQuarantinePath+"="+quarantine.QuarantinePath,
	)
}

func receiveRepositoryUsesObjectFormat(ctx context.Context, restricted git.RepositoryFacade, environ []string, expected git.ObjectFormat) bool {
	stdout, _, err := gitcmd.NewCommand("hash-object", "--stdin").
		WithEnv(environ).
		WithRepo(restricted).
		WithStdinBytes(nil).
		WithTimeout(receiveGitVerificationTimeout).
		RunStdString(ctx)
	if err != nil {
		return false
	}
	objectID, objectIDErr := git.NewIDFromString(strings.TrimSpace(stdout))
	return objectIDErr == nil && objectID.Type().Name() == expected.Name()
}

func receiveObjectIsCommit(ctx context.Context, restricted git.RepositoryFacade, environ []string, objectID string) bool {
	stdout, _, err := gitcmd.NewCommand("cat-file", "-t").
		AddDynamicArguments(objectID).
		WithEnv(environ).
		WithRepo(restricted).
		WithTimeout(receiveGitVerificationTimeout).
		RunStdString(ctx)
	return err == nil && strings.TrimSpace(stdout) == string(git.ObjectCommit)
}

func receiveAncestor(ctx context.Context, restricted git.RepositoryFacade, environ []string, ancestor, descendant string) bool {
	_, _, err := gitcmd.NewCommand("merge-base", "--is-ancestor").
		AddDynamicArguments(ancestor, descendant).
		WithEnv(environ).
		WithRepo(restricted).
		WithTimeout(receiveGitVerificationTimeout).
		RunStdString(ctx)
	return err == nil
}

func receiveCurrentRef(ctx context.Context, restricted git.RepositoryFacade, environ []string, ref string, objectFormat git.ObjectFormat) (string, bool, bool) {
	stdout, _, err := gitcmd.NewCommand("rev-parse", "--verify", "--quiet").
		AddDynamicArguments(ref).
		WithEnv(environ).
		WithRepo(restricted).
		WithTimeout(receiveGitVerificationTimeout).
		RunStdString(ctx)
	if err != nil {
		if gitcmd.IsErrorExitCode(err, 1) {
			return "", false, true
		}
		return "", false, false
	}
	objectID := strings.TrimSpace(stdout)
	if !git.IsStringValidObjectID(objectFormat, objectID) {
		return "", false, false
	}
	return objectID, true, true
}
