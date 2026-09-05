package orgs

import (
	"net/http"
	"strings"

	"nautilus/internal/database/organizations"
	"nautilus/internal/database/users"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/mux"
	"nautilus/internal/validators"
)

func (o *OrgMux) CreateInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	orgExternalID, ok := mux.PathParam(r, "orgId")
	if !ok || !validators.ValidateUUID(orgExternalID) {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}

	org, err := organizations.GetByExternalID(ctx, o.db, orgExternalID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if org == nil {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}

	if org.Personal {
		httputil.Error(ctx, w, InviteError(ErrCannotInvitePersonalOrg))
		return
	}

	user := users.FromContext(ctx)
	member, err := organizations.GetMemberByUserAndOrg(ctx, o.db, user.ID, org.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if member == nil || !member.Role.CanManageMembers() {
		httputil.Error(ctx, w, ErrInviteForbidden)
		return
	}

	var form CreateInviteForm
	err = httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	existingUser, err := users.GetByEmail(ctx, o.db, form.Email)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if existingUser != nil {
		existingMember, err := organizations.GetMemberByUserAndOrg(ctx, o.db, existingUser.ID, org.ID)
		if err != nil {
			httputil.Error(ctx, w, err)
			return
		}
		if existingMember != nil {
			httputil.Error(ctx, w, InviteError(ErrAlreadyMember))
			return
		}
	}

	token, invite, err := organizations.CreateInvite(
		ctx,
		o.db,
		org.ID,
		user.ID,
		form.Email,
		form.Role,
		organizations.DefaultInviteExpiration,
	)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	res := httputil.Map{
		"invite": invite,
		"token":  token,
	}
	httputil.JSON(ctx, w, res, http.StatusCreated)
}

func (o *OrgMux) ListInvites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	orgExternalID, _ := mux.PathParam(r, "orgId")
	if !validators.ValidateUUID(orgExternalID) {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}

	org, err := organizations.GetByExternalID(ctx, o.db, orgExternalID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if org == nil {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}

	user := users.FromContext(ctx)
	member, err := organizations.GetMemberByUserAndOrg(ctx, o.db, user.ID, org.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if member == nil {
		httputil.Error(ctx, w, ErrInviteForbidden)
		return
	}

	invites, err := organizations.ListInvitesByOrg(ctx, o.db, org.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	res := httputil.Map{
		"invites": invites,
	}
	httputil.JSON(ctx, w, res)
}

func (o *OrgMux) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	orgExternalID, _ := mux.PathParam(r, "orgId")
	inviteExternalID, _ := mux.PathParam(r, "inviteId")

	if !validators.ValidateUUID(orgExternalID) || !validators.ValidateUUID(inviteExternalID) {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}

	org, err := organizations.GetByExternalID(ctx, o.db, orgExternalID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if org == nil {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}

	user := users.FromContext(ctx)
	member, err := organizations.GetMemberByUserAndOrg(ctx, o.db, user.ID, org.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if member == nil || !member.Role.CanManageMembers() {
		httputil.Error(ctx, w, ErrInviteForbidden)
		return
	}

	invite, err := organizations.GetInviteByExternalID(ctx, o.db, inviteExternalID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if invite == nil || invite.OrganizationID != org.ID {
		httputil.Error(ctx, w, ErrInviteNotFound)
		return
	}

	err = organizations.RevokeInvite(ctx, o.db, invite.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (o *OrgMux) RedeemInvite(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token, _ := mux.PathParam(r, "token")
	token = strings.TrimSpace(token)

	if token == "" {
		httputil.Error(ctx, w, InviteError(ErrInvalidToken))
		return
	}

	invite, err := organizations.VerifyInvite(ctx, o.db, token)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if invite == nil {
		httputil.Error(ctx, w, InviteError(ErrInvalidToken))
		return
	}

	user := users.FromContext(ctx)

	if !user.Email.Set || !strings.EqualFold(user.Email.Data, invite.Email) {
		httputil.Error(ctx, w, InviteError(ErrEmailMismatch))
		return
	}

	member, err := organizations.RedeemInvite(ctx, o.db, token, user.ID)
	if err != nil {
		if strings.Contains(err.Error(), "already a member") {
			httputil.Error(ctx, w, InviteError(ErrAlreadyMember))
			return
		}
		httputil.Error(ctx, w, err)
		return
	}
	if member == nil {
		// Token became invalid between Verify and Redeem (race condition)
		httputil.Error(ctx, w, InviteError(ErrInvalidToken))
		return
	}

	org, err := organizations.Get(ctx, o.db, invite.OrganizationID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	res := httputil.Map{
		"member":       member,
		"organization": org,
	}
	httputil.JSON(ctx, w, res, http.StatusCreated)
}
