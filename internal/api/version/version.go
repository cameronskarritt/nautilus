package version

type Version string

const (
	VersionLatest Version = Version20260101

	Version20260101 Version = "2026-01-01"
)

var versions = []Version{
	Version20260101,
}
var versionMap map[Version]bool

func init() {
	versionMap = make(map[Version]bool, len(versions))
	for _, v := range versions {
		versionMap[v] = true
	}
}

func (vs Version) Matches(other Version) bool {
	// Strings order lexicographically - we can get more complex here if we so desire
	return vs >= other
}

func (vs Version) Validate() bool {
	_, ok := versionMap[vs]
	return ok
}

// Validator validates version strings
type Validator interface {
	Validate(Version) bool
}

// DefaultValidator uses the global versionMap
type DefaultValidator struct{}

func (DefaultValidator) Validate(vs Version) bool {
	_, ok := versionMap[vs]
	return ok
}
