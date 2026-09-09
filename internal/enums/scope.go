package enums

type Scope string

const (
	ScopeRead  Scope = "read"
	ScopeWrite Scope = "write"
)

func (s Scope) IsValid() bool {
	return s == ScopeRead || s == ScopeWrite
}
