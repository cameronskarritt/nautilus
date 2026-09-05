package version

import (
	"net/http"
	"slices"

	"nautilus/internal/errors"
	"nautilus/internal/httputil"
)

type Versions map[Version]http.HandlerFunc

type versionMux struct {
	mapping   map[Version]http.HandlerFunc
	versions  []Version
	validator Validator
}

// MuxOption is a functional option for configuring versionMux
type MuxOption func(*versionMux)

// WithValidator sets a custom Validator for the mux
func WithValidator(v Validator) MuxOption {
	return func(m *versionMux) {
		m.validator = v
	}
}

func (mux *versionMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	version := FromContext(ctx)

	if !mux.validator.Validate(version) {
		httputil.Error(ctx, w, Error(ErrInvalidVersion))
		return
	}

	handler, ok := mux.mapping[version]
	if ok {
		handler(w, r)
		return
	}

	match := -1
	for i := 0; i < len(mux.versions); i++ {
		if !version.Matches(mux.versions[i]) {
			break
		}
		match = i
	}

	if match == -1 {
		httputil.Error(ctx, w, errors.New("No matching version found")) // TODO(CLS): Add error details
		return
	}

	mux.mapping[mux.versions[match]](w, r)
}

func Use(vs Versions, opts ...MuxOption) http.Handler {
	versioner := &versionMux{
		mapping:   vs,
		versions:  make([]Version, 0, len(vs)),
		validator: DefaultValidator{},
	}

	for _, opt := range opts {
		opt(versioner)
	}

	for key := range vs {
		versioner.versions = append(versioner.versions, key)
	}
	slices.Sort(versioner.versions)

	return versioner
}
