package webpush

import "nautilus/internal/notifier"

// Urgency indicates to the push service how important a message is to the user.
// This can be used by the push service to help conserve the battery life of a
// user's device by only waking up for important messages when battery is low.
type Urgency string

const (
	UrgencyVeryLow Urgency = "very-low"
	UrgencyLow     Urgency = "low"
	UrgencyNormal  Urgency = "normal"
	UrgencyHigh    Urgency = "high"
)

func isValidUrgency(urgency Urgency) bool {
	switch urgency {
	case UrgencyVeryLow, UrgencyLow, UrgencyNormal, UrgencyHigh:
		return true
	}
	return false
}

func urgencyFromNotifier(u notifier.Urgency) Urgency {
	switch u {
	case notifier.UrgencyLow:
		return UrgencyLow
	case notifier.UrgencyHigh:
		return UrgencyHigh
	default:
		return UrgencyNormal
	}
}
