package enums

type MailTemplate string

func (mt MailTemplate) String() string {
	return string(mt)
}

const (
	MailTemplateWelcome            MailTemplate = "welcome"
	MailTemplateEmailVerification  MailTemplate = "email-verification"
	MailTemplateInitiateRecovery   MailTemplate = "initiate-recovery"
	MailTemplateCompleteRecovery   MailTemplate = "complete-recovery"
	MailTemplateWrongAuthProvider  MailTemplate = "wrong-auth-provider"
	MailTemplatePasswordUpdated    MailTemplate = "password-updated"
	MailTemplateChangeEmailRequest MailTemplate = "email-change-request"
	MailTemplateEmailUpdated       MailTemplate = "email-updated"
)
