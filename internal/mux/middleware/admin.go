package middleware

import (
	"net/http"

	"nautilus/internal/database"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/log"
	"nautilus/internal/mux"
)

const orgOverrideHeader = "X-Organization-Slug"

var ErrAdminRequired = errors.NewHTTPError(
	http.StatusForbidden,
	"Access denied",
	errors.ErrorDetail{
		Message: "admin access required",
		Code:    errors.ErrorCodeADMIN01,
	},
)

var ErrOrgNotFound = errors.NewHTTPError(
	http.StatusNotFound,
	"Organization not found",
	errors.ErrorDetail{
		Message: "organization not found",
		Code:    errors.ErrorCodeORG01,
	},
)

var ErrAssumedOrgMismatch = errors.NewHTTPError(
	http.StatusConflict,
	"Assumed organization mismatch",
	errors.ErrorDetail{
		Message: "session organization does not match request header",
		Code:    errors.ErrorCodeADMIN02,
	},
)

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := users.FromContext(ctx)

		if user == nil || !user.Admin {
			httputil.Error(ctx, w, ErrAdminRequired)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func AdminOrgOverride(db database.Database) mux.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			user := users.FromContext(ctx)

			// Only apply to admin users
			if user == nil || !user.Admin {
				next.ServeHTTP(w, r)
				return
			}

			// Check for assumed org in session first, then fall back to header
			assumedOrgID := sessions.AssumedOrgIDFromContext(ctx)
			headerSlug := r.Header.Get(orgOverrideHeader)

			if !assumedOrgID.Set && headerSlug == "" {
				next.ServeHTTP(w, r)
				return
			}

			logger := log.FromContext(ctx)

			// If both session and header are present, validate they match
			if assumedOrgID.Set && headerSlug != "" {
				headerOrg, err := organizations.GetBySlug(ctx, db, headerSlug)
				if err != nil {
					httputil.Error(ctx, w, err)
					return
				}
				if headerOrg == nil || headerOrg.ID != assumedOrgID.Data {
					httputil.Error(ctx, w, ErrAssumedOrgMismatch)
					return
				}
			}

			var org *organizations.Organization
			var err error

			if assumedOrgID.Set {
				// Load org by ID from session
				org, err = organizations.Get(ctx, db, assumedOrgID.Data)
			} else {
				// Fall back to header (for backwards compatibility)
				org, err = organizations.GetBySlug(ctx, db, headerSlug)
			}

			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			if org == nil {
				httputil.Error(ctx, w, ErrOrgNotFound)
				return
			}

			// Create a virtual OrgMember (not persisted)
			virtualMember := &organizations.Member{
				ID:             0,
				UserID:         user.ID,
				OrganizationID: org.ID,
				Role:           organizations.RoleOwner,
			}

			ctx = organizations.WithMemberContext(ctx, virtualMember)
			ctx = organizations.WithContext(ctx, org)

			logger = logger.With("admin_org_override", org.Slug)
			ctx = log.WithContext(ctx, logger)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
