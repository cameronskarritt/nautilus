package oauth

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"unicode"

	"nautilus/internal/config"
	"nautilus/internal/database"
	"nautilus/internal/database/oauth"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
)

type Mux struct{ db database.Database }

func NewMux(db database.Database) *Mux { return &Mux{db: db} }
func Issuer() string {
	return strings.TrimRight(config.Get("MCP_BASE_URL", "http://localhost:8082"), "/")
}
func Resource() string { return Issuer() + "/mcp" }
func endpoint() string {
	return strings.TrimRight(config.Get("API_BASE_URL", "http://localhost:8080/api"), "/") + "/mcp/oauth"
}
func appURL() string {
	return strings.TrimRight(config.Get("APP_BASE_URL", "http://localhost:5173"), "/")
}

func Metadata(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	respond(w, http.StatusOK, map[string]any{
		"issuer": Issuer(), "authorization_endpoint": endpoint() + "/authorize", "token_endpoint": endpoint() + "/token",
		"registration_endpoint": endpoint() + "/register", "revocation_endpoint": endpoint() + "/revoke",
		"response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code", "refresh_token"},
		"token_endpoint_auth_methods_supported": []string{"none"}, "revocation_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported": []enums.Scope{enums.ScopeRead, enums.ScopeWrite}, "code_challenge_methods_supported": []string{"S256"},
	})
}
func (m *Mux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)
	sub.Get("/authorize", m.Authorize)
	sub.Post("/register", m.Register)
	sub.Post("/token", m.Token)
	sub.Post("/revoke", m.Revoke)
	sub.Use(middleware.RequireSession(m.db))
	sub.Use(m.consentSession)
	sub.Get("/request", m.Request)
	sub.Post("/authorize", m.Consent)
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Default().Error("Unable to write OAuth response", "error", err)
	}
}
func failure(w http.ResponseWriter, status int, code string) {
	respond(w, status, map[string]string{"error": code})
}
func storeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, oauth.ErrInvalidClient):
		failure(w, 400, "invalid_client")
	case errors.Is(err, oauth.ErrInvalidGrant):
		failure(w, 400, "invalid_grant")
	case errors.Is(err, oauth.ErrInvalidScope):
		failure(w, 400, "invalid_scope")
	default:
		log.FromContext(r.Context()).Error("OAuth operation failed", "error", err)
		failure(w, 500, "server_error")
	}
}
func jsonBody(w http.ResponseWriter, r *http.Request, v any) bool {
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/json" {
		failure(w, 400, "invalid_request")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		failure(w, 400, "invalid_request")
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		failure(w, 400, "invalid_request")
		return false
	}
	return true
}
func safeRedirect(raw string) bool {
	if len(raw) > 2048 || strings.ContainsFunc(raw, func(r rune) bool { return r == '\\' || unicode.IsControl(r) }) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") || u.Opaque != "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())
}
func scope(raw string) (string, bool) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return string(enums.ScopeRead), true
	}
	for _, s := range fields {
		if !enums.Scope(s).IsValid() {
			return "", false
		}
	}
	slices.Sort(fields)
	return strings.Join(slices.Compact(fields), " "), true
}
func valuesValid(v url.Values) bool {
	for _, values := range v {
		if len(values) != 1 || len(values[0]) > 4096 {
			return false
		}
	}
	return len(v) <= 24
}
func (m *Mux) validate(w http.ResponseWriter, r *http.Request, v url.Values) *oauth.Client {
	if !valuesValid(v) || v.Get("client_id") == "" || !safeRedirect(v.Get("redirect_uri")) {
		failure(w, 400, "invalid_request")
		return nil
	}
	client, err := oauth.GetClient(r.Context(), m.db, v.Get("client_id"))
	if err != nil {
		storeError(w, r, err)
		return nil
	}
	if client == nil || !slices.Contains(client.RedirectURIs, v.Get("redirect_uri")) {
		failure(w, 400, "invalid_client")
		return nil
	}
	challenge, err := base64.RawURLEncoding.DecodeString(v.Get("code_challenge"))
	if v.Get("response_type") != "code" || v.Get("code_challenge_method") != "S256" || err != nil || len(challenge) != 32 || v.Get("resource") != Resource() {
		failure(w, 400, "invalid_request")
		return nil
	}
	normalized, ok := scope(v.Get("scope"))
	if !ok {
		failure(w, 400, "invalid_scope")
		return nil
	}
	v.Set("scope", normalized)
	return client
}
func query(w http.ResponseWriter, r *http.Request) url.Values {
	if len(r.URL.RawQuery) > 16384 {
		failure(w, 400, "invalid_request")
		return nil
	}
	v, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		failure(w, 400, "invalid_request")
		return nil
	}
	return v
}
func (m *Mux) Authorize(w http.ResponseWriter, r *http.Request) {
	v := query(w, r)
	if v == nil || m.validate(w, r, v) == nil {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, appURL()+"/mcp/authorize?"+v.Encode(), http.StatusFound)
}
func (m *Mux) consentSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		user := users.FromContext(ctx)
		member := organizations.MemberFromContext(ctx)
		org := organizations.FromContext(ctx)
		if user == nil || member == nil || org == nil || member.UserID != user.ID || member.OrganizationID != org.ID {
			failure(w, 403, "access_denied")
			return
		}
		session, err := sessions.GetByID(ctx, m.db, sessions.FromContext(ctx))
		if err != nil {
			storeError(w, r, err)
			return
		}
		if session == nil || session.AssumedBy.Set || session.AssumedOrgID.Set {
			failure(w, 403, "access_denied")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (m *Mux) Request(w http.ResponseWriter, r *http.Request) {
	v := query(w, r)
	if v == nil {
		return
	}
	client := m.validate(w, r, v)
	if client == nil {
		return
	}
	org := organizations.FromContext(r.Context())
	respond(w, 200, map[string]any{"client_name": client.Name, "redirect_uri": v.Get("redirect_uri"), "scope": v.Get("scope"), "user_id": users.FromContext(r.Context()).ExternalID, "organization": map[string]string{"id": org.ExternalID, "name": org.Name}})
}
func (m *Mux) Consent(w http.ResponseWriter, r *http.Request) {
	origin, err := url.Parse(appURL())
	if err != nil || r.Header.Get("Origin") != origin.Scheme+"://"+origin.Host {
		failure(w, 403, "access_denied")
		return
	}
	var body map[string]string
	if !jsonBody(w, r, &body) {
		return
	}
	v := make(url.Values, len(body))
	for k, value := range body {
		v.Set(k, value)
	}
	if m.validate(w, r, v) == nil {
		return
	}
	if v.Get("user_id") != users.FromContext(r.Context()).ExternalID || v.Get("organization_id") != organizations.FromContext(r.Context()).ExternalID {
		failure(w, 403, "access_denied")
		return
	}
	decision := v.Get("decision")
	if decision != "allow" && decision != "deny" {
		failure(w, 400, "invalid_request")
		return
	}
	target, _ := url.Parse(v.Get("redirect_uri"))
	q := target.Query()
	q.Del("code")
	q.Del("error")
	q.Del("state")
	if decision == "deny" {
		q.Set("error", "access_denied")
	} else {
		code, err := oauth.CreateGrant(r.Context(), m.db, users.FromContext(r.Context()).ID, organizations.MemberFromContext(r.Context()).ID, &oauth.GrantOptions{ClientID: v.Get("client_id"), RedirectURI: v.Get("redirect_uri"), Resource: v.Get("resource"), Scope: v.Get("scope"), Challenge: v.Get("code_challenge")})
		if err != nil {
			storeError(w, r, err)
			return
		}
		q.Set("code", code)
	}
	if v.Has("state") {
		q.Set("state", v.Get("state"))
	}
	target.RawQuery = q.Encode()
	respond(w, 200, map[string]string{"redirect_url": target.String()})
}

type registration struct {
	Name          string   `json:"client_name"`
	RedirectURIs  []string `json:"redirect_uris"`
	GrantTypes    []string `json:"grant_types"`
	ResponseTypes []string `json:"response_types"`
	AuthMethod    string   `json:"token_endpoint_auth_method"`
}

func (m *Mux) Register(w http.ResponseWriter, r *http.Request) {
	var form registration
	if !jsonBody(w, r, &form) {
		return
	}
	form.Name = strings.TrimSpace(form.Name)
	if form.Name == "" {
		form.Name = "MCP client"
	}
	if len(form.Name) > 200 || len(form.RedirectURIs) == 0 || len(form.RedirectURIs) > 10 || form.AuthMethod != "" && form.AuthMethod != "none" {
		failure(w, 400, "invalid_client_metadata")
		return
	}
	for _, uri := range form.RedirectURIs {
		if !safeRedirect(uri) {
			failure(w, 400, "invalid_redirect_uri")
			return
		}
	}
	if len(form.GrantTypes) == 0 {
		form.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	if len(form.ResponseTypes) == 0 {
		form.ResponseTypes = []string{"code"}
	}
	if len(form.GrantTypes) > 2 || !slices.Contains(form.GrantTypes, "authorization_code") || len(form.ResponseTypes) != 1 || form.ResponseTypes[0] != "code" {
		failure(w, 400, "invalid_client_metadata")
		return
	}
	for _, grant := range form.GrantTypes {
		if grant != "authorization_code" && grant != "refresh_token" {
			failure(w, 400, "invalid_client_metadata")
			return
		}
	}
	client, err := oauth.RegisterClient(r.Context(), m.db, form.Name, form.RedirectURIs)
	if err != nil {
		storeError(w, r, err)
		return
	}
	respond(w, 201, map[string]any{"client_id": client.ID, "client_name": client.Name, "redirect_uris": client.RedirectURIs, "grant_types": form.GrantTypes, "response_types": form.ResponseTypes, "token_endpoint_auth_method": "none"})
}
func tokenForm(w http.ResponseWriter, r *http.Request) url.Values {
	typ, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || typ != "application/x-www-form-urlencoded" {
		failure(w, 400, "invalid_request")
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := r.ParseForm(); err != nil || !valuesValid(r.PostForm) || r.PostForm.Get("client_id") == "" {
		failure(w, 400, "invalid_request")
		return nil
	}
	if r.Header.Get("Authorization") != "" || r.PostForm.Has("client_secret") {
		failure(w, 400, "invalid_client")
		return nil
	}
	return r.PostForm
}
func (m *Mux) Token(w http.ResponseWriter, r *http.Request) {
	v := tokenForm(w, r)
	if v == nil {
		return
	}
	if v.Get("grant_type") == "refresh_token" && !v.Has("resource") {
		v.Set("resource", Resource())
	}
	if v.Get("resource") != Resource() {
		failure(w, 400, "invalid_target")
		return
	}
	var tokens *oauth.Tokens
	var err error
	switch v.Get("grant_type") {
	case "authorization_code":
		if v.Get("code") == "" || v.Get("redirect_uri") == "" || v.Get("code_verifier") == "" {
			failure(w, 400, "invalid_request")
			return
		}
		tokens, err = oauth.ExchangeCode(r.Context(), m.db, v.Get("client_id"), v.Get("code"), v.Get("redirect_uri"), v.Get("resource"), v.Get("code_verifier"))
	case "refresh_token":
		if v.Get("refresh_token") == "" {
			failure(w, 400, "invalid_request")
			return
		}
		tokens, err = oauth.Refresh(r.Context(), m.db, v.Get("client_id"), v.Get("refresh_token"), v.Get("resource"), v.Get("scope"))
	default:
		failure(w, 400, "unsupported_grant_type")
		return
	}
	if err != nil {
		storeError(w, r, err)
		return
	}
	respond(w, 200, tokens)
}
func (m *Mux) Revoke(w http.ResponseWriter, r *http.Request) {
	v := tokenForm(w, r)
	if v == nil {
		return
	}
	if v.Get("token") == "" {
		failure(w, 400, "invalid_request")
		return
	}
	if err := oauth.Revoke(r.Context(), m.db, v.Get("client_id"), v.Get("token")); err != nil {
		storeError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
}
