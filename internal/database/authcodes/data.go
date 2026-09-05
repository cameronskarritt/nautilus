package authcodes

type ChangeEmailData struct {
	// Store the old email to establish a papertrail -
	// it costs ~nothing and may come in handy later
	OldEmail string `json:"old_email"`
	NewEmail string `json:"new_email"`
}
