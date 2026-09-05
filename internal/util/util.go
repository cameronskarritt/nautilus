package util

func Deref[T any](t *T) T {
	var zero T
	if t == nil {
		return zero
	}
	return *t
}
