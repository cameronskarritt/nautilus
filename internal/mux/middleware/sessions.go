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

var ErrNoSessionFound = errors.NewHTTPError(
	http.StatusUnauthorized,
	"Unable to process request",
	errors.ErrorDetail{
		Message: "user has no session",
		Code:    errors.ErrorCodeSESS01,
	},
)

func RequireSession(db database.Database) mux.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			logger := log.FromContext(ctx)

			token, err := sessions.FromCookie(r)
			if err != nil {
				// Note(CLS): As of writing, this is the only possible error FromCookie can return
				if errors.Is(err, http.ErrNoCookie) || errors.Is(err, sessions.ErrNoAuthorizationHeader) {
					httputil.Error(ctx, w, ErrNoSessionFound)
					return
				}
				httputil.Error(ctx, w, err)
				return
			}

			session, err := sessions.Get(ctx, db, token)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			if session == nil {
				httputil.Error(ctx, w, ErrNoSessionFound)
				return
			}

			user, err := users.Get(ctx, db, session.UserID)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			if user == nil {
				httputil.Error(ctx, w, ErrNoSessionFound)
				return
			}

			logger = logger.With(
				"session_id", session.ID,
				"user_id", session.UserID,
			)
			if session.AssumedBy.Set {
				logger = logger.With("assumed_by", session.AssumedBy.Data)
			}

			ctx = log.WithContext(ctx, logger)
			ctx = users.WithContext(ctx, user)
			ctx = sessions.WithContext(ctx, session.ID)
			ctx = sessions.WithAssumedOrgID(ctx, session.AssumedOrgID)

			// Load org context if session has an org_member_id
			if session.OrgMemberID.Set {
				orgMember, err := organizations.GetMember(ctx, db, session.OrgMemberID.Data)
				if err != nil {
					httputil.Error(ctx, w, err)
					return
				}
				if orgMember != nil {
					if orgMember.UserID != session.UserID {
						httputil.Error(ctx, w, ErrNoSessionFound)
						return
					}
					ctx = organizations.WithMemberContext(ctx, orgMember)
					logger = logger.With("org_member_id", orgMember.ID)

					org, err := organizations.Get(ctx, db, orgMember.OrganizationID)
					if err != nil {
						httputil.Error(ctx, w, err)
						return
					}
					if org != nil {
						ctx = organizations.WithContext(ctx, org)
						logger = logger.With("organization_id", org.ID)
					}
				}
				ctx = log.WithContext(ctx, logger)
			}

			r = r.WithContext(ctx)

			next.ServeHTTP(w, r)
		})
	}
}
