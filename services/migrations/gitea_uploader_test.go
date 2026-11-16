// Copyright 2019 The Gitea Authors. All rights reserved.
// Copyright 2018 Jonas Franz. All rights reserved.
// SPDX-License-Identifier: MIT

package migrations

import (
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	actions_model "gitea.dev/models/actions"
	auth_model "gitea.dev/models/auth"
	"gitea.dev/models/db"
	git_model "gitea.dev/models/git"
	issues_model "gitea.dev/models/issues"
	"gitea.dev/models/organization"
	"gitea.dev/models/perm"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	webhook_model "gitea.dev/models/webhook"
	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/graceful"
	base "gitea.dev/modules/migration"
	"gitea.dev/modules/optional"
	"gitea.dev/modules/structs"
	webhook_module "gitea.dev/modules/webhook"
	"gitea.dev/services/auth/source/oauth2"
	repo_service "gitea.dev/services/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/builder"
)

func TestGiteaUploadRepo(t *testing.T) {
	// FIXME: Since no accesskey or user/password will trigger rate limit of github, just skip
	t.Skip()

	unittest.PrepareTestEnv(t)

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

	ctx := t.Context()
	downloader, err := NewGithubDownloaderV3(ctx, "https://github.com", "", "", "", "go-xorm", "builder")
	require.NoError(t, err)
	var (
		repoName = "builder-" + time.Now().Format("2006-01-02-15-04-05")
		uploader = NewGiteaLocalUploader(graceful.GetManager().HammerContext(), user, user.Name, repoName)
	)

	err = migrateRepository(t.Context(), user, downloader, uploader, base.MigrateOptions{
		CloneAddr:    "https://github.com/go-xorm/builder",
		RepoName:     repoName,
		AuthUsername: "",

		Wiki:         true,
		Issues:       true,
		Milestones:   true,
		Labels:       true,
		Releases:     true,
		Comments:     true,
		PullRequests: true,
		Private:      true,
		Mirror:       false,
	}, nil)
	assert.NoError(t, err)

	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{OwnerID: user.ID, Name: repoName})
	assert.True(t, repo_service.HasWiki(ctx, repo))
	assert.Equal(t, repo_model.RepositoryReady, repo.Status)

	milestones, err := db.Find[issues_model.Milestone](t.Context(), issues_model.FindMilestoneOptions{
		RepoID:   repo.ID,
		IsClosed: optional.Some(false),
	})
	assert.NoError(t, err)
	assert.Len(t, milestones, 1)

	milestones, err = db.Find[issues_model.Milestone](t.Context(), issues_model.FindMilestoneOptions{
		RepoID:   repo.ID,
		IsClosed: optional.Some(true),
	})
	assert.NoError(t, err)
	assert.Empty(t, milestones)

	labels, err := issues_model.GetLabelsByRepoID(ctx, repo.ID, "", db.ListOptions{})
	assert.NoError(t, err)
	assert.Len(t, labels, 12)

	releases, err := db.Find[repo_model.Release](t.Context(), repo_model.FindReleasesOptions{
		ListOptions: db.ListOptions{
			PageSize: 10,
			Page:     0,
		},
		IncludeTags: true,
		RepoID:      repo.ID,
	})
	assert.NoError(t, err)
	assert.Len(t, releases, 8)

	releases, err = db.Find[repo_model.Release](t.Context(), repo_model.FindReleasesOptions{
		ListOptions: db.ListOptions{
			PageSize: 10,
			Page:     0,
		},
		IncludeTags: false,
		RepoID:      repo.ID,
	})
	assert.NoError(t, err)
	assert.Len(t, releases, 1)

	issues, err := issues_model.Issues(t.Context(), &issues_model.IssuesOptions{
		RepoIDs:  []int64{repo.ID},
		IsPull:   optional.Some(false),
		SortType: "oldest",
	})
	assert.NoError(t, err)
	assert.Len(t, issues, 15)
	assert.NoError(t, issues[0].LoadDiscussComments(t.Context()))
	assert.Empty(t, issues[0].Comments)

	pulls, _, err := issues_model.PullRequests(t.Context(), repo.ID, &issues_model.PullRequestsOptions{
		SortType: "oldest",
	})
	assert.NoError(t, err)
	assert.Len(t, pulls, 30)
	assert.NoError(t, pulls[0].LoadIssue(t.Context()))
	assert.NoError(t, pulls[0].Issue.LoadDiscussComments(t.Context()))
	assert.Len(t, pulls[0].Issue.Comments, 2)
}

func TestCreateReleases(t *testing.T) {
	unittest.PrepareTestEnv(t)
	sourceRelease := &base.Release{Name: "assets", Latest: true, Assets: []*base.ReleaseAsset{
		{Name: "gone", Size: new(0), DownloadCount: new(0), DownloadFunc: func() (io.ReadCloser, error) { return nil, errors.New("gone") }},
		{Name: "kept", Size: new(0), DownloadCount: new(0), DownloadFunc: func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("data")), nil }},
	}}

	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	uploader := NewGiteaLocalUploader(t.Context(), doer, doer.Name, "repo1")
	uploader.repo = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	gitRepo, err := git.OpenRepository(t.Context(), uploader.repo)
	require.NoError(t, err)
	defer gitRepo.Close()
	uploader.gitRepo = gitRepo
	_, err = db.GetEngine(t.Context()).Delete(&repo_model.Release{RepoID: 1, TagName: "v1.1"})
	require.NoError(t, err)
	require.NoError(t, uploader.CreateReleases(t.Context(), sourceRelease, &base.Release{TagName: "v1.1", Created: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)}))
	require.NoError(t, uploader.SyncTags(t.Context()))
	commit, err := gitRepo.GetTagCommit(t.Context(), "v1.1")
	require.NoError(t, err)
	assert.Equal(t, commit.Committer.When.Unix(), int64(unittest.AssertExistsAndLoadBean(t, &repo_model.Release{RepoID: 1, TagName: "v1.1"}).CreatedUnix))
	assert.Zero(t, unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1}).LatestReleaseID)
	require.NoError(t, uploader.CreateReleases(t.Context(), &base.Release{TagName: "newer", Created: time.Now().Add(time.Hour)}))
	require.NoError(t, uploader.SyncTags(t.Context()))
	release := unittest.AssertExistsAndLoadBean(t, &repo_model.Release{RepoID: 1, Title: "assets"})
	unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1, LatestReleaseID: release.ID})
	unittest.AssertExistsAndLoadBean(t, &repo_model.Attachment{ReleaseID: release.ID, Name: "kept", Size: 4})
	unittest.AssertNotExistsBean(t, &repo_model.Attachment{ReleaseID: release.ID, Name: "gone"})

	dumper := &RepositoryDumper{baseDir: t.TempDir(), opts: base.MigrateOptions{ReleaseAssets: true}}
	defer dumper.Close()
	require.NoError(t, dumper.CreateReleases(t.Context(), sourceRelease))
	require.Len(t, sourceRelease.Assets, 1)
	assert.Equal(t, "kept", sourceRelease.Assets[0].Name)
}

func TestGiteaUploadRemapLocalUser(t *testing.T) {
	unittest.PrepareTestEnv(t)
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	ctx := t.Context()
	repoName := "migrated"
	uploader := NewGiteaLocalUploader(ctx, doer, doer.Name, repoName)
	// call remapLocalUser
	uploader.sameApp = true

	for _, tc := range []struct {
		source         base.Release
		expectedUserID int64
	}{
		//
		// The externalID does not match any existing user, everything
		// belongs to the doer
		//
		{base.Release{PublisherID: 1234567, PublisherName: "username"}, doer.ID},
		//
		// The externalID matches a known user but the name does not match,
		// everything belongs to the doer
		//
		{base.Release{PublisherID: user.ID, PublisherName: "username"}, doer.ID},
		//
		// The externalID and externalName match an existing user, everything
		// belongs to the existing user
		//
		{base.Release{PublisherID: user.ID, PublisherName: user.Name}, user.ID},
	} {
		target := repo_model.Release{}
		uploader.userMap = make(map[int64]int64)
		err := uploader.remapUser(ctx, &tc.source, &target)
		assert.NoError(t, err)
		assert.Equal(t, tc.expectedUserID, target.GetUserID())
	}

	t.Run("TimelineLabelAndMilestoneComments", func(t *testing.T) {
		issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 1})
		label := unittest.AssertExistsAndLoadBean(t, &issues_model.Label{ID: 1})
		milestone := unittest.AssertExistsAndLoadBean(t, &issues_model.Milestone{ID: 1})

		uploader := NewGiteaLocalUploader(ctx, doer, doer.Name, "migrated")
		uploader.issues[issue.Index] = issue
		uploader.labels[label.Name] = label
		uploader.milestones[milestone.Name] = milestone.ID

		require.NoError(t, uploader.CreateComments(ctx,
			&base.Comment{IssueIndex: issue.Index, CommentType: "label", Content: "1", Meta: map[string]any{"LabelName": label.Name}},
			&base.Comment{IssueIndex: issue.Index, CommentType: "label", Meta: map[string]any{"LabelName": "deleted-upstream"}},
			&base.Comment{IssueIndex: issue.Index, CommentType: "milestone", Meta: map[string]any{"MilestoneTitle": milestone.Name}},
			&base.Comment{IssueIndex: issue.Index, CommentType: "milestone", Meta: map[string]any{"MilestoneTitle": milestone.Name, "Removed": true}},
			&base.Comment{IssueIndex: issue.Index, CommentType: "delete_branch", Content: "feature"},
			&base.Comment{IssueIndex: issue.Index, CommentType: "delete_branch", Content: strings.Repeat("a", 256)},
		))

		unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{IssueID: issue.ID, Type: issues_model.CommentTypeLabel, LabelID: label.ID, Content: "1"})
		unittest.AssertCountByCond(t, "comment", builder.Eq{
			"issue_id": issue.ID,
			"type":     issues_model.CommentTypeLabel,
			"label_id": 0,
		}, 0)
		unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{IssueID: issue.ID, Type: issues_model.CommentTypeMilestone, MilestoneID: milestone.ID})
		unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{IssueID: issue.ID, Type: issues_model.CommentTypeMilestone, OldMilestoneID: milestone.ID})
		assert.Empty(t, unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{IssueID: issue.ID, Type: issues_model.CommentTypeDeleteBranch, OldRef: "feature"}).Content)
		assert.Empty(t, unittest.AssertExistsAndLoadBean(t, &issues_model.Comment{IssueID: issue.ID, Type: issues_model.CommentTypeDeleteBranch, OldRef: strings.Repeat("a", 255)}).Content)
	})
}

func TestGiteaUploadRemapExternalUser(t *testing.T) {
	unittest.PrepareTestEnv(t)
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	ctx := t.Context()
	repoName := "migrated"
	uploader := NewGiteaLocalUploader(ctx, doer, doer.Name, repoName)
	uploader.gitServiceType = structs.GiteaService
	// call remapExternalUser
	uploader.sameApp = false

	externalID := int64(1234567)
	externalName := "username"
	source := base.Release{
		PublisherID:   externalID,
		PublisherName: externalName,
	}

	//
	// When there is no user linked to the external ID, the migrated data is authored
	// by the doer
	//
	uploader.userMap = make(map[int64]int64)
	target := repo_model.Release{}
	err := uploader.remapUser(ctx, &source, &target)
	assert.NoError(t, err)
	assert.Equal(t, doer.ID, target.GetUserID())

	//
	// Link the external ID to an existing user
	//
	linkedUser := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	externalLoginUser := &user_model.ExternalLoginUser{
		ExternalID:    strconv.FormatInt(externalID, 10),
		UserID:        linkedUser.ID,
		LoginSourceID: 0,
		Provider:      structs.GiteaService.Name(),
	}
	err = user_model.LinkExternalToUser(t.Context(), linkedUser, externalLoginUser)
	assert.NoError(t, err)

	//
	// When a user is linked to the external ID, it becomes the author of
	// the migrated data
	//
	uploader.userMap = make(map[int64]int64)
	target = repo_model.Release{}
	err = uploader.remapUser(ctx, &source, &target)
	assert.NoError(t, err)
	assert.Equal(t, linkedUser.ID, target.GetUserID())
}

func TestGiteaUploadReviewCommentResolved(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	pull := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	require.NoError(t, pull.LoadIssue(ctx))

	uploader := NewGiteaLocalUploader(ctx, doer, repo.OwnerName, repo.Name)
	var err error
	uploader.repo = repo
	uploader.gitRepo, err = git.OpenRepository(ctx, repo)
	require.NoError(t, err)
	defer uploader.Close()
	uploader.issues[pull.Issue.Index] = pull.Issue

	headCommitID, err := uploader.gitRepo.GetBranchCommitID(ctx, repo.DefaultBranch)
	require.NoError(t, err)
	require.NoError(t, git.UpdateRef(ctx, repo, pull.GetGitHeadRefName(), headCommitID))

	require.NoError(t, uploader.CreateReviews(ctx, &base.Review{
		IssueIndex: pull.Issue.Index,
		ReviewerID: doer.ID,
		State:      base.ReviewStateCommented,
		Comments: []*base.ReviewComment{
			{Content: "resolved thread", TreePath: "README.md", Line: 1, Resolved: true},
			{Content: "open thread", TreePath: "README.md", Line: 1},
		},
	}))

	comments, err := issues_model.FindComments(ctx, &issues_model.FindCommentsOptions{IssueID: pull.Issue.ID, Type: issues_model.CommentTypeCode})
	require.NoError(t, err)
	resolved := map[string]bool{}
	for _, c := range comments {
		if c.Content == "resolved thread" || c.Content == "open thread" {
			resolved[c.Content] = c.ResolveDoerID == doer.ID
		}
	}
	assert.Equal(t, map[string]bool{"resolved thread": true, "open thread": false}, resolved)
}

func TestGiteaUploadForkPullRequestHeadInRepository(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})

	uploader := NewGiteaLocalUploader(ctx, doer, repo.OwnerName, repo.Name)
	var err error
	uploader.repo = repo
	uploader.gitRepo, err = git.OpenRepository(ctx, repo)
	require.NoError(t, err)
	defer uploader.Close()

	headCommitID, err := uploader.gitRepo.GetBranchCommitID(ctx, repo.DefaultBranch)
	require.NoError(t, err)

	// the fork cannot be reached, but its head commit is already in the repository
	pr := &base.PullRequest{
		Number: 99,
		State:  "open",
		Head:   base.PullRequestBranch{OwnerName: "fork-owner", RepoName: "repo", Ref: "feature", SHA: headCommitID, CloneURL: "https://fork.invalid/fork-owner/repo.git"},
		Base:   base.PullRequestBranch{OwnerName: repo.OwnerName, RepoName: repo.Name, Ref: repo.DefaultBranch},
	}
	pr.PatchURL = "file:///nonexistent.patch"
	pr.EnsuredSafe = true
	require.True(t, pr.IsForkPullRequest())

	head, err := uploader.updateGitForPullRequest(ctx, pr)
	require.NoError(t, err)
	assert.Equal(t, "fork-owner/feature", head)
	assert.True(t, uploader.gitRepo.IsBranchExist(ctx, "fork-owner/feature"))
	branchCommitID, err := uploader.gitRepo.GetBranchCommitID(ctx, "fork-owner/feature")
	require.NoError(t, err)
	assert.Equal(t, headCommitID, branchCommitID)
	refCommitID, err := uploader.gitRepo.GetRefCommitID(ctx, pr.GetGitHeadRefName())
	require.NoError(t, err)
	assert.Equal(t, headCommitID, refCommitID)
	remotes, _, err := gitcmd.NewCommand("remote").WithRepo(repo).RunStdString(ctx)
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(remotes), "no remote is added for a local head")
}

func TestGiteaUploadReviewCommentsWithoutHead(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	pull := unittest.AssertExistsAndLoadBean(t, &issues_model.PullRequest{ID: 1})
	require.NoError(t, pull.LoadIssue(ctx))

	uploader := NewGiteaLocalUploader(ctx, doer, repo.OwnerName, repo.Name)
	var err error
	uploader.repo = repo
	uploader.gitRepo, err = git.OpenRepository(ctx, repo)
	require.NoError(t, err)
	defer uploader.Close()
	uploader.issues[pull.Issue.Index] = pull.Issue
	require.NoError(t, git.RemoveRef(ctx, repo, pull.GetGitHeadRefName()))

	opts := issues_model.FindReviewOptions{IssueID: pull.Issue.ID}
	reviewsBefore, err := issues_model.CountReviews(ctx, opts)
	require.NoError(t, err)

	require.NoError(t, uploader.CreateReviews(ctx, &base.Review{
		IssueIndex: pull.Issue.Index,
		ReviewerID: doer.ID,
		State:      base.ReviewStateCommented,
		Comments:   []*base.ReviewComment{{Content: "lost anchor", TreePath: "README.md", Line: -3}},
	}))

	comments, err := issues_model.FindComments(ctx, &issues_model.FindCommentsOptions{IssueID: pull.Issue.ID, Type: issues_model.CommentTypeComment})
	require.NoError(t, err)
	var contents []string
	for _, c := range comments {
		contents = append(contents, c.Content)
	}
	assert.Contains(t, contents, "Comment on `README.md` line 3:\n\nlost anchor")
	reviewsAfter, err := issues_model.CountReviews(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, reviewsBefore, reviewsAfter) // no empty review is left behind
}

func TestGiteaUploadSettings(t *testing.T) {
	unittest.PrepareTestEnv(t)
	ctx := t.Context()
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2}) // owner of org3
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 5})
	repo.OriginalURL = "https://github.com/harbour-club/tides"
	linked := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	link := func(user *user_model.User, externalID string, urls *oauth2.CustomURLMapping) {
		source := &auth_model.Source{Type: auth_model.OAuth2, Name: "github " + externalID, IsActive: true, Cfg: &oauth2.Source{Provider: "github", CustomURLMapping: urls}}
		require.NoError(t, auth_model.CreateSource(ctx, source))
		require.NoError(t, user_model.LinkExternalToUser(ctx, user, &user_model.ExternalLoginUser{ExternalID: externalID, UserID: user.ID, LoginSourceID: source.ID, Provider: "github"}))
	}
	link(linked, "7", nil)
	// the same id on a GitHub Enterprise server is another person
	link(unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5}), "8", &oauth2.CustomURLMapping{AuthURL: "https://github.example.com/login/oauth/authorize"})

	uploader := NewGiteaLocalUploader(ctx, doer, "org3", repo.Name)
	uploader.gitServiceType = structs.GithubService
	uploader.repo = repo
	require.NoError(t, uploader.CreateSettings(ctx, &base.RepoSettings{
		BranchProtections: []*base.BranchProtection{{Pattern: "release/*", BlockPush: true, AllowDeletion: true, RequiredApprovals: 2, Unsupported: []string{"require linear history"}}},
		Collaborators: []*base.Collaborator{
			{ExternalID: "7", Login: "niamh", Permission: "write"},
			{ExternalID: "8", Login: "user5", Permission: "admin"}, // neither the matching name nor the matching id proves it is the local user5
		},
		Teams: []*base.Team{
			{Name: "Team 1", Slug: "team1", Permission: "write"},
			{Name: "test_team", Slug: "test_team", Permission: "read"}, // the local team writes
			{Name: "Ghosts", Slug: "ghosts", Permission: "admin"},
		},
		Webhooks: []*base.Webhook{
			{URL: "https://hooks.example.com/services/T0/secret-token", ContentType: "json", Events: []string{"push"}, UnsupportedEvents: []string{"deployment"}},
			{URL: "https://deploy.example.com/hook", UnsupportedEvents: []string{"deployment"}},
		},
		Variables: []*base.ActionsVariable{{Name: "NODE_VERSION", Value: "24"}, {Name: "GITHUB_SHADOW", Value: "x"}},
		Notes:     []base.SettingsNote{{Kind: base.SettingsNoteSecret, Name: "DEPLOY_KEY"}},
	}))

	rule := unittest.AssertExistsAndLoadBean(t, &git_model.ProtectedBranch{RepoID: repo.ID, RuleName: "release/*"})
	assert.False(t, rule.CanPush)
	assert.False(t, rule.CanDelete)
	assert.EqualValues(t, 2, rule.RequiredApprovals)

	unittest.AssertExistsAndLoadBean(t, &repo_model.Collaboration{RepoID: repo.ID, UserID: linked.ID, Mode: perm.AccessModeWrite})
	unittest.AssertNotExistsBean(t, &repo_model.Collaboration{RepoID: repo.ID, UserID: 5})
	assert.True(t, organization.HasTeamRepo(ctx, repo.OwnerID, 2, repo.ID))
	assert.False(t, organization.HasTeamRepo(ctx, repo.OwnerID, 7, repo.ID))

	webhook := unittest.AssertExistsAndLoadBean(t, &webhook_model.Webhook{RepoID: repo.ID})
	assert.False(t, webhook.IsActive)
	assert.True(t, webhook.HasEvent(webhook_module.HookEventPush))
	unittest.AssertExistsAndLoadBean(t, &actions_model.ActionVariable{RepoID: repo.ID, Name: "NODE_VERSION", Data: "24"})

	report, err := repo_model.GetMigrationReport(ctx, repo.ID)
	require.NoError(t, err)
	assert.Equal(t, &base.SettingsReport{
		BranchProtections: 1, Collaborators: 1, Teams: 1, Webhooks: 1, Variables: 1,
		Notes: []base.SettingsNote{
			{Kind: base.SettingsNoteSecret, Name: "DEPLOY_KEY"},
			{Kind: base.SettingsNoteBranchProtection, Name: "release/*", Detail: "require linear history"},
			{Kind: base.SettingsNoteCollaborator, Name: "user5", Detail: "admin"},
			{Kind: base.SettingsNoteTeamAccess, Name: "test_team", Detail: "read"},
			{Kind: base.SettingsNoteTeam, Name: "Ghosts", Detail: "admin"},
			{Kind: base.SettingsNoteWebhook, Name: "hooks.example.com", Detail: "deployment"},
			{Kind: base.SettingsNoteWebhookSkipped, Name: "deploy.example.com", Detail: "deployment"},
			{Kind: base.SettingsNoteVariable, Name: "GITHUB_SHADOW"},
		},
	}, report.Report)
}
