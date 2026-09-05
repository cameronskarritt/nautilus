package color

import (
	"fmt"
)

type ANSICode string

const (
	reset ANSICode = "\033[0m"

	Black   ANSICode = "30"
	Red     ANSICode = "31"
	Green   ANSICode = "32"
	Yellow  ANSICode = "33"
	Blue    ANSICode = "34"
	Magenta ANSICode = "35"
	Cyan    ANSICode = "36"
	White   ANSICode = "37"

	// Bright variants
	BrightBlack   ANSICode = "90"
	BrightRed     ANSICode = "91"
	BrightGreen   ANSICode = "92"
	BrightYellow  ANSICode = "93"
	BrightBlue    ANSICode = "94"
	BrightMagenta ANSICode = "95"
	BrightCyan    ANSICode = "96"
	BrightWhite   ANSICode = "97"

	// bold      ANSICode = "\u001b[1m"
	// dim       ANSICode = "\u001b[2m"
	// italics   ANSICode = "\u001b[3m"
	// underline ANSICode = "\u001b[4m"
)

func Style(s string, code ANSICode) string {
	return fmt.Sprint("\033[", code, "m", s, reset)
}
