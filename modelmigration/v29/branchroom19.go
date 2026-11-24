// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"
)

func BindBranchroomTokenSponsors(_ context.Context, x base.EngineMigration) error {
	const botUserType = 4
	const tokenOwner = `SELECT account.name FROM access_token
		JOIN ` + "`user`" + ` AS account ON account.id = access_token.uid
		WHERE access_token.id = branchroom_token_identity.token_id
		AND account.type <> ? AND LOWER(branchroom_token_identity.sponsor) <> account.lower_name`
	_, err := x.Exec("UPDATE branchroom_token_identity SET sponsor = ("+tokenOwner+") WHERE sponsor <> '' AND EXISTS ("+tokenOwner+")", botUserType, botUserType)
	return err
}
