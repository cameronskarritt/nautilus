package mail

import (
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

var subjects = map[enums.MailTemplate]string{
	enums.MailTemplateWelcome:            "Welcome to nautilus!",
	enums.MailTemplateEmailVerification:  "Verify your account",
	enums.MailTemplateInitiateRecovery:   "Recover your account",
	enums.MailTemplateWrongAuthProvider:  "Recover your account",
	enums.MailTemplateCompleteRecovery:   "Your account has been recovered",
	enums.MailTemplatePasswordUpdated:    "A change has been made to your account",
	enums.MailTemplateChangeEmailRequest: "Confirm your email change request",
	enums.MailTemplateEmailUpdated:       "A change has been made to your account",
}

func GetSubject(template enums.MailTemplate) (string, error) {
	subject, exists := subjects[template]
	if !exists {
		return "", errors.Errorf("no subject set for template: %s", template)
	}
	return subject, nil
}
