package validators

import (
	"net"
	"net/mail"
	"regexp"
	"strings"

	"nautilus/internal/errors"
)

var uuidRegex = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

func ValidateUUID(uuid string) bool {
	return uuidRegex.MatchString(uuid)
}

func ValidateEmail(email string) bool {
	if len(email) > 320 {
		return false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil {
		return false
	}

	if addr.Address != email {
		return false
	}

	parts := strings.Split(addr.Address, "@")
	if len(parts) != 2 {
		return false
	}

	domain := parts[1]
	if ip := net.ParseIP(domain); ip != nil {
		return false
	}

	if !strings.Contains(domain, ".") || strings.HasSuffix(domain, ".") || strings.HasPrefix(domain, ".") {
		return false
	}
	if strings.ContainsAny(domain, "[]") {
		return false
	}

	return true
}

func ValidatePassword(password string) error {
	if len(password) < 8 {
		return errors.ErrorDetail{
			Message: "password must be more than 8 characters",
			Code:    errors.ErrorCodeAUTH06,
			Field:   "password",
		}
	}
	if len(password) > 256 {
		return errors.ErrorDetail{
			Message: "password must be less than 256 characters",
			Code:    errors.ErrorCodeAUTH07,
			Field:   "password",
		}
	}

	return nil
}

func IsAlphanumeric(s string) bool {
	for _, r := range s {
		// Note(CLS): we can't use unicode.IsDigit and unicode.IsLetter here
		// since it matches true against non-latin alphabets
		if (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func ValidateUsername(username string) error {
	if len(username) < 3 {
		return errors.ErrorDetail{
			Message: "username must be at least 3 characters",
			Code:    errors.ErrorCodeUSER02,
			Field:   "username",
		}
	}

	if len(username) > 20 {
		return errors.ErrorDetail{
			Message: "username must be 20 characters or fewer",
			Code:    errors.ErrorCodeUSER03,
			Field:   "username",
		}
	}

	if !IsAlphanumeric(username) {
		return errors.ErrorDetail{
			Message: "username must only contain numbers and letters",
			Code:    errors.ErrorCodeUSER04,
			Field:   "username",
		}
	}

	return nil
}
