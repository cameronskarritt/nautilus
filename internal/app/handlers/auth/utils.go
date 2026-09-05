package auth

import (
	"fmt"
	"math/rand/v2"
)

func randomUsername() string {
	n := 100_000 + rand.IntN(900_000)
	return fmt.Sprint("user", n)
}

func loginCounterKey(id int) string {
	return fmt.Sprintf("login.%d", id)
}
