// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migrations

import (
	"errors"
	"fmt"
	"net/url"

	"gitea.dev/modules/log"
	base "gitea.dev/modules/migration"

	"github.com/google/go-github/v93/github"
)

const maxSettingsPages = 50

var errSettingsPageLimit = errors.New("settings page limit reached")

// settingsStatusError keeps the HTTP status of a failed settings request, for sources whose client drops it from the error
type settingsStatusError struct {
	status int
	err    error
}

func (e *settingsStatusError) Error() string { return e.err.Error() }
func (e *settingsStatusError) Unwrap() error { return e.err }

// readableSettings notes what the source would not hand over and carries on, a missing permission never fails a migration
func readableSettings[T any](settings *base.RepoSettings, area string, items []T, err error) []T {
	if err == nil {
		return items
	}
	if errors.Is(err, errSettingsPageLimit) {
		settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNotePageLimit, Name: area})
		return items
	}
	log.Warn("migrating %s is not possible: %v", area, err)
	note := base.SettingsNote{Kind: base.SettingsNoteUnreadable, Name: area}
	if response, ok := errors.AsType[*github.ErrorResponse](err); ok && response.Response != nil {
		note.Detail = fmt.Sprintf("HTTP %d", response.Response.StatusCode)
	} else if failed, ok := errors.AsType[*settingsStatusError](err); ok {
		note.Detail = fmt.Sprintf("HTTP %d", failed.status)
	} else if failed, ok := errors.AsType[*bbsStatusError](err); ok {
		note.Detail = fmt.Sprintf("HTTP %d", failed.Status)
	}
	settings.Notes = append(settings.Notes, note)
	return nil
}

// webhookHost is all of a webhook URL that is safe to show, chat services put the credential in the path
func webhookHost(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Host
}
