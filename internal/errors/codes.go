package errors

import "fmt"

type ErrorCode string

func (ec ErrorCode) String() string {
	return string(ec)
}

func httpCode(status int) ErrorCode {
	code := fmt.Sprintf("HTTP-%d", status)
	return ErrorCode(code)
}

const (
	ErrorCodeJSON01 = "JSON-01" // Request body exceeds max size
	ErrorCodeJSON02 = "JSON-02" // JSON syntax error
	ErrorCodeJSON03 = "JSON-03" // Request body cannot be empty
	ErrorCodeJSON04 = "JSON-04" // Unexpected EOF in JSON
	ErrorCodeJSON05 = "JSON-05" // Invalid field type in JSON
	ErrorCodeJSON06 = "JSON-06" // Multiple JSON objects in request body
	ErrorCodeJSON07 = "JSON-07" // Invalid Content-Type header

	ErrorCodeAUTH01 = "AUTH-01" // Email address already in use
	ErrorCodeAUTH02 = "AUTH-02" // Email cannot be empty
	ErrorCodeAUTH03 = "AUTH-03" // Invalid email address format
	ErrorCodeAUTH04 = "AUTH-04" // Invalid email or password (prevents user enumeration)
	ErrorCodeAUTH05 = "AUTH-05" // Invalid or expired authentication code
	ErrorCodeAUTH06 = "AUTH-06" // Password too short (less than 8 characters)
	ErrorCodeAUTH07 = "AUTH-07" // Password too long (more than 256 characters)
	ErrorCodeAUTH08 = "AUTH-08" // Username unavailable
	ErrorCodeAUTH09 = "AUTH-09" // Old password mismatch
	ErrorCodeAUTH10 = "AUTH-10" // Too many failed login attempts
	ErrorCodeAUTH11 = "AUTH-11" // New email cannot be the same as current email
	ErrorCodeAUTH12 = "AUTH-12" // User already verified
	ErrorCodeAUTH13 = "AUTH-13" // Wrong authentication provider
	ErrorCodeAUTH14 = "AUTH-14" // Token cannot be empty
	ErrorCodeAUTH15 = "AUTH-15" // Email not configured
	ErrorCodeAUTH16 = "AUTH-16" // Invalid SSO provider
	ErrorCodeAUTH17 = "AUTH-17" // SSO provider error
	ErrorCodeAUTH18 = "AUTH-18" // SSO missing authorization code
	ErrorCodeAUTH19 = "AUTH-19" // SSO invalid state
	ErrorCodeAUTH20 = "AUTH-20" // SSO provider mismatch
	ErrorCodeAUTH21 = "AUTH-21" // SSO token exchange failed
	ErrorCodeAUTH22 = "AUTH-22" // SSO email already exists
	ErrorCodeAUTH23 = "AUTH-23" // SSO server error
	ErrorCodeAUTH24 = "AUTH-24" // Invalid TOTP code
	ErrorCodeAUTH25 = "AUTH-25" // TOTP not enabled
	ErrorCodeAUTH26 = "AUTH-26" // TOTP already enabled
	ErrorCodeAUTH27 = "AUTH-27" // MFA required (login pending)
	ErrorCodeAUTH28 = "AUTH-28" // Invalid MFA token
	ErrorCodeAUTH29 = "AUTH-29" // TOTP code cannot be empty
	ErrorCodeAUTH30 = "AUTH-30" // Password cannot be empty
	ErrorCodeAUTH31 = "AUTH-31" // Active SSO organization membership required
	ErrorCodeAUTH32 = "AUTH-32" // SSO redirect URL is not allowed

	ErrorCodeUSER01 = "USER-01" // Missing user identifier (id or username)
	ErrorCodeUSER02 = "USER-02" // Username too short (less than 3 characters)
	ErrorCodeUSER03 = "USER-03" // Username too long (more than 20 characters)
	ErrorCodeUSER04 = "USER-04" // Username contains invalid characters (must be alphanumeric)
	ErrorCodeUSER05 = "USER-05" // Username already taken

	ErrorCodeSESS01 = "SESS-01" // User has no session
	ErrorCodeSESS02 = "SESS-02" // Current session is not an assumed session
	ErrorCodeSESS03 = "SESS-03" // No admin session to restore

	ErrorCodeADMIN01 = "ADMIN-01" // Admin access required
	ErrorCodeADMIN02 = "ADMIN-02" // Assumed org mismatch between session and header
	ErrorCodeADMIN03 = "ADMIN-03" // Feature flag name is required
	ErrorCodeADMIN04 = "ADMIN-04" // Feature flag name already exists
	ErrorCodeADMIN05 = "ADMIN-05" // Feature flag not found
	ErrorCodeADMIN06 = "ADMIN-06" // Feature flag ID is invalid

	ErrorCodeORG01 = "ORG-01" // Organization not found
	ErrorCodeORG02 = "ORG-02" // Not a member of this organization

	ErrorCodeINVITE01 = "INVITE-01" // Invite not found or expired
	ErrorCodeINVITE02 = "INVITE-02" // No permission to manage invites for organization
	ErrorCodeINVITE03 = "INVITE-03" // Email is required for invite
	ErrorCodeINVITE04 = "INVITE-04" // Invalid email for invite
	ErrorCodeINVITE05 = "INVITE-05" // Invalid role (must be member, admin, or viewer)
	ErrorCodeINVITE06 = "INVITE-06" // Cannot invite users as owner
	ErrorCodeINVITE07 = "INVITE-07" // User is already a member of organization
	ErrorCodeINVITE08 = "INVITE-08" // Email does not match invite
	ErrorCodeINVITE09 = "INVITE-09" // Invalid or expired invite token
	ErrorCodeINVITE10 = "INVITE-10" // Cannot invite users to personal organization

	ErrorCodeAPI01 = "API-01" // Invalid or unsupported API version

	ErrorCodeAPIKEY01 = "APIKEY-01" // Organization context is required
	ErrorCodeAPIKEY02 = "APIKEY-02" // API key management permission is required
	ErrorCodeAPIKEY03 = "APIKEY-03" // API key name is required
	ErrorCodeAPIKEY04 = "APIKEY-04" // API key scopes are required
	ErrorCodeAPIKEY05 = "APIKEY-05" // API key scope is invalid
	ErrorCodeAPIKEY06 = "APIKEY-06" // API key name already exists
	ErrorCodeAPIKEY07 = "APIKEY-07" // API key not found
	ErrorCodeAPIKEY08 = "APIKEY-08" // API key name is too long
	ErrorCodeAPIKEY09 = "APIKEY-09" // API key authentication failed
	ErrorCodeAPIKEY10 = "APIKEY-10" // API key scope is insufficient
	ErrorCodeDOC01    = "DOC-01"    // Document organization context is required
	ErrorCodeDOC02    = "DOC-02"    // Document permission is required
	ErrorCodeDOC03    = "DOC-03"    // Document pagination cursor is invalid
	ErrorCodeDOC04    = "DOC-04"    // Document upload has invalid multipart file structure
	ErrorCodeDOC05    = "DOC-05"    // Document upload content type is unsupported
	ErrorCodeDOC06    = "DOC-06"    // Document upload exceeds size limits
	ErrorCodeDOC07    = "DOC-07"    // Document filename is invalid
	ErrorCodeDOC08    = "DOC-08"    // Document upload storage is unavailable
	ErrorCodeDOC09    = "DOC-09"    // Document upload workflow service is unavailable
	ErrorCodeDOC10    = "DOC-10"    // Scan image format, data, or dimensions are invalid
	ErrorCodeDOC11    = "DOC-11"    // Scan upload exceeds the page count limit
	ErrorCodeDOC12    = "DOC-12"    // Extracted document text is unavailable
	ErrorCodeDOC13    = "DOC-13"    // Document read storage is unavailable

)

const (
	ErrorCodeWEBHOOK01 = "WEBHOOK-01" // Webhook organization required
	ErrorCodeWEBHOOK02 = "WEBHOOK-02" // Webhook permission required
	ErrorCodeWEBHOOK03 = "WEBHOOK-03" // Webhook name invalid
	ErrorCodeWEBHOOK04 = "WEBHOOK-04" // Webhook URL invalid
	ErrorCodeWEBHOOK05 = "WEBHOOK-05" // Webhook event types invalid
	ErrorCodeWEBHOOK06 = "WEBHOOK-06" // Webhook update empty
	ErrorCodeWEBHOOK07 = "WEBHOOK-07" // Webhook enabled invalid
	ErrorCodeWEBHOOK08 = "WEBHOOK-08" // Webhook cursor invalid
	ErrorCodeWEBHOOK09 = "WEBHOOK-09" // Webhook limit reached
	ErrorCodeWEBHOOK10 = "WEBHOOK-10" // Webhook rotation overlap active
)

const (
	ErrorCodeWEBHOOK11 = "WEBHOOK-11" // Webhook is disabled
	ErrorCodeWEBHOOK12 = "WEBHOOK-12" // Webhook test workflow unavailable
)
