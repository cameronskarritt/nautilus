package users

import (
	"net/http"

	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/featureflags"
	"nautilus/internal/httputil"
	"nautilus/internal/validators"
)

func (u *UserMux) GetUserExternal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	query := r.URL.Query()
	id, username := query.Get("id"), query.Get("username")
	if id == "" && username == "" {
		err := FetchUserError(ErrMissingIdentifier)
		httputil.Error(ctx, w, err)
		return
	}

	var user *users.UserExternal
	var err error

	if id != "" {
		if !validators.ValidateUUID(id) {
			httputil.Error(ctx, w, errors.ErrNotFound)
			return
		}

		user, err = users.GetExternal(ctx, u.db, id)
		if err != nil {
			httputil.Error(ctx, w, err)
			return
		}
	} else if username != "" {
		user, err = users.GetExternalUsername(ctx, u.db, username)
		if err != nil {
			httputil.Error(ctx, w, err)
			return
		}
	}

	if user == nil {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	res := httputil.Map{
		"user": user,
	}
	httputil.JSON(ctx, w, res)
}

func (u *UserMux) Me(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := users.FromContext(ctx)
	org := organizations.FromContext(ctx)
	assumedOrgID := sessions.AssumedOrgIDFromContext(ctx)
	assumed := assumedOrgID.Set && org != nil

	userFlags, err := u.flags.List(ctx, enums.FeatureFlagObjectTypeUser, user.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	flags := userFlags
	if org != nil {
		orgFlags, err := u.flags.List(ctx, enums.FeatureFlagObjectTypeOrganization, org.ID)
		if err != nil {
			httputil.Error(ctx, w, err)
			return
		}
		flags = featureflags.MergeFlags(orgFlags, userFlags)
	}

	res := httputil.Map{
		"user":         user,
		"organization": org,
		"assumed":      assumed,
		"flags":        flags,
	}
	httputil.JSON(ctx, w, res)
}

func (u *UserMux) UpdateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := users.FromContext(ctx)

	var form UpdateUserForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	if form.Username.Set {
		var currentUsername string
		if user.Username.Set {
			currentUsername = user.Username.Data
		}

		if form.Username.Data != currentUsername {
			exists, err := users.UsernameExists(ctx, u.db, form.Username.Data)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			if exists {
				httputil.Error(ctx, w, UserUpdateError(ErrUsernameExists))
				return
			}

			err = users.UpdateUsername(ctx, u.db, user.ID, form.Username.Data)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
		}
	}

	user, err = users.Get(ctx, u.db, user.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	res := httputil.Map{
		"user": user,
	}
	httputil.JSON(ctx, w, res)
}
