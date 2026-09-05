package authcodes

import (
	"encoding/json"

	"nautilus/internal/errors"
)

type AuthCode struct {
	UserID int
	Data   []byte
}

func (code AuthCode) UnmarshalData(v any) error {
	err := json.Unmarshal(code.Data, v)
	if err != nil {
		return errors.Wrap(err, "unable to unmarshal authcode data")
	}

	return nil
}
