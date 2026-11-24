// Copyright 2023 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package context

import user_model "gitea.dev/models/user"

// IsUserSiteAdmin returns true if current user is a site admin
func (ctx *Context) IsUserSiteAdmin() bool {
	return ctx.IsSigned && ctx.Doer.IsAdmin && len(user_model.GetDoerRepositoryIDs(ctx.Doer)) == 0
}
