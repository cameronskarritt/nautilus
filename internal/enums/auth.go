package enums

type AuthCodeType string

const (
	AuthCodeVerification AuthCodeType = "verification"
	AuthCodeRecovery     AuthCodeType = "recovery"
	AuthCodeEmailChange  AuthCodeType = "email-change"
)

type AuthProvider string

const (
	AuthProviderLocal     AuthProvider = "local"
	AuthProviderGoogle    AuthProvider = "google"
	AuthProviderMicrosoft AuthProvider = "microsoft"
	AuthProviderGitHub    AuthProvider = "github"
	AuthProviderApple     AuthProvider = "apple"
)

func (ap AuthProvider) String() string {
	return string(ap)
}
