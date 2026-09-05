package admin

import (
	"database/sql"
	"net/http"
	"strconv"

	"nautilus/internal/database/auditlogs"
	"nautilus/internal/database/featureflags"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/log"
	"nautilus/internal/mux"
	"nautilus/internal/optional"
)

func (a *AdminMux) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	orgs, err := organizations.ListAll(ctx, a.db)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	res := httputil.Map{
		"organizations": orgs,
	}
	httputil.JSON(ctx, w, res)
}

func (a *AdminMux) ListFlags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	flags, err := featureflags.ListAll(ctx, a.db)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	creatorIDs := make([]int, 0)
	seen := make(map[int]bool)
	for _, flag := range flags {
		if flag.CreatedByID.Set && !seen[flag.CreatedByID.Data] {
			creatorIDs = append(creatorIDs, flag.CreatedByID.Data)
			seen[flag.CreatedByID.Data] = true
		}
	}

	creators, err := users.GetByIDs(ctx, a.db, creatorIDs)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	for _, flag := range flags {
		if flag.CreatedByID.Set {
			if creator, ok := creators[flag.CreatedByID.Data]; ok {
				flag.CreatedBy = &featureflags.FlagCreator{
					ID:       creator.ExternalID,
					Email:    creator.Email.Data,
					Username: creator.Username.Data,
				}
			}
		}
	}

	res := httputil.Map{
		"flags": flags,
	}
	httputil.JSON(ctx, w, res)
}

func (a *AdminMux) CreateFlag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var form CreateFlagForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	flag, err := featureflags.Create(ctx, a.db, form.Name, form.Description, form.Enabled)
	if err != nil {
		if errors.Is(err, featureflags.ErrFlagNameExists) {
			httputil.Error(ctx, w, FeatureFlagError(ErrFlagNameExists))
			return
		}
		httputil.Error(ctx, w, err)
		return
	}

	res := httputil.Map{
		"flag": flag,
	}
	httputil.JSON(ctx, w, res)
}

func (a *AdminMux) UpdateFlag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr, ok := mux.PathParam(r, "id")
	if !ok {
		httputil.Error(ctx, w, ErrFeatureFlagNotFound)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		httputil.Error(ctx, w, ErrFeatureFlagNotFound)
		return
	}

	var form UpdateFlagForm
	err = httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	opts := featureflags.UpdateOptions{
		Description: form.Description,
		Enabled:     form.Enabled,
	}

	flag, err := featureflags.Update(ctx, a.db, id, opts)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	if flag == nil {
		httputil.Error(ctx, w, ErrFeatureFlagNotFound)
		return
	}

	res := httputil.Map{
		"flag": flag,
	}
	httputil.JSON(ctx, w, res)
}

func (a *AdminMux) DeleteFlag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr, ok := mux.PathParam(r, "id")
	if !ok {
		httputil.Error(ctx, w, ErrFeatureFlagNotFound)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		httputil.Error(ctx, w, ErrFeatureFlagNotFound)
		return
	}

	err = featureflags.Delete(ctx, a.db, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			httputil.Error(ctx, w, ErrFeatureFlagNotFound)
			return
		}
		httputil.Error(ctx, w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *AdminMux) GetOrganizationFlags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	orgIDStr, ok := mux.PathParam(r, "orgId")
	if !ok {
		httputil.Error(ctx, w, ErrOrganizationNotFound)
		return
	}

	org, err := organizations.GetByExternalID(ctx, a.db, orgIDStr)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if org == nil {
		httputil.Error(ctx, w, ErrOrganizationNotFound)
		return
	}

	flagIDs, err := featureflags.ListAssociatedFlagIDs(ctx, a.db, enums.FeatureFlagObjectTypeOrganization, org.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	res := httputil.Map{
		"flag_ids": flagIDs,
	}
	httputil.JSON(ctx, w, res)
}

func (a *AdminMux) UpdateOrganizationFlag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)

	orgIDStr, ok := mux.PathParam(r, "orgId")
	if !ok {
		httputil.Error(ctx, w, ErrOrganizationNotFound)
		return
	}

	org, err := organizations.GetByExternalID(ctx, a.db, orgIDStr)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if org == nil {
		httputil.Error(ctx, w, ErrOrganizationNotFound)
		return
	}

	var form UpdateOrgFlagForm
	err = httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	flag, err := featureflags.Get(ctx, a.db, form.FlagID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if flag == nil {
		httputil.Error(ctx, w, ErrFeatureFlagNotFound)
		return
	}

	err = featureflags.SetAssociation(ctx, a.db, enums.FeatureFlagObjectTypeOrganization, org.ID, form.FlagID, form.Enabled)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	user := users.FromContext(ctx)
	if user != nil {
		payload := map[string]any{
			"org_id":    org.ExternalID,
			"org_name":  org.Name,
			"flag_id":   flag.ID,
			"flag_name": flag.Name,
			"enabled":   form.Enabled,
		}
		_, err = auditlogs.Create(ctx, a.db, user.ID, enums.AuditTypeOrgFlagUpdate, optional.Set(org.ID), optional.Set[any](payload))
		if err != nil {
			// Audit logging should not block the flag update.
			logger.Error("unable to create audit log", "error", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
