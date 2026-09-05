package mux

import (
	"fmt"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"

	"nautilus/internal/errors"
)

type regexChild struct {
	param string
	regex *regexp.Regexp
	node  *node
}

type node struct {
	children      map[string]*node
	handlers      map[string]http.Handler
	param         string
	paramChild    *node
	regexChildren []regexChild
}

func newNode() *node {
	return &node{
		children:      make(map[string]*node),
		regexChildren: make([]regexChild, 0),
		handlers:      make(map[string]http.Handler),
	}
}

type tree struct {
	mu   sync.RWMutex
	root *node
}

func (t *tree) addRoute(method string, path string, handler http.Handler) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if path == "/" {
		t.root.handlers[method] = handler
		return
	}

	current := t.root
	parts := strings.Split(path, "/")[1:]

	for _, part := range parts {
		// Handle index paths on a subrouter
		if part == "" {
			break
		}

		param, re, err := parsePattern(part)
		if err != nil {
			panic(err)
		}
		if re != nil {
			// Check if a regex child with this param and regex already exists
			var child *node
			var found bool
			for i := range current.regexChildren {
				if current.regexChildren[i].param == param && current.regexChildren[i].regex.String() == re.String() {
					child = current.regexChildren[i].node
					found = true
					break
				}
			}
			if !found {
				n := newNode()
				current.regexChildren = append(current.regexChildren, regexChild{
					param: param,
					regex: re,
					node:  n,
				})
				child = n
			}
			current = child
			continue
		}

		if param != "" {
			if current.paramChild == nil {
				n := newNode()
				n.param = param
				current.paramChild = n
			}
			current = current.paramChild
			continue
		}

		child, exists := current.children[part]
		if !exists {
			n := newNode()
			current.children[part] = n
			child = n
		}
		current = child
	}

	current.handlers[method] = handler
}

func (t *tree) find(path string) (*node, map[string]string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if path == "/" {
		return t.root, nil, true
	}

	current := t.root
	parts := strings.Split(path, "/")[1:]

	params := make(map[string]string)

PathLoop:
	for _, part := range parts {
		if part == "" {
			return current, nil, true
		}
		if child, exists := current.children[part]; exists {
			current = child
			continue PathLoop
		}

		if current.paramChild != nil {
			params[current.paramChild.param] = part
			current = current.paramChild
			continue PathLoop
		}

		// Iterate in order - first match wins
		for _, regexChild := range current.regexChildren {
			if regexChild.regex != nil && regexChild.regex.MatchString(part) {
				params[regexChild.param] = part
				current = regexChild.node
				continue PathLoop
			}
		}

		return nil, nil, false
	}
	return current, params, true
}

type Router struct {
	tree                    *tree
	prefix                  string
	middleware              []Middleware
	notFoundHandler         http.Handler
	methodNotAllowedHandler http.Handler
	optionsHandler          http.Handler
}

func New(config ...Config) *Router {
	r := &Router{
		tree: &tree{
			root: newNode(),
		},
		notFoundHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
		}),
		methodNotAllowedHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}),
		optionsHandler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}

	if len(config) > 0 {
		r.UseConfig(config[0])
	}

	r.notFoundHandler = r.applyMiddleware(r.notFoundHandler)
	r.methodNotAllowedHandler = r.applyMiddleware(r.methodNotAllowedHandler)
	r.optionsHandler = r.applyMiddleware(r.optionsHandler)

	return r
}

func (router *Router) SubRouter(prefix string) *Router {
	mws := make([]Middleware, len(router.middleware))
	copy(mws, router.middleware)

	return &Router{
		tree:                    router.tree,
		prefix:                  router.prefix + prefix,
		middleware:              mws,
		notFoundHandler:         router.notFoundHandler,
		methodNotAllowedHandler: router.methodNotAllowedHandler,
		optionsHandler:          router.optionsHandler,
	}
}

func (router *Router) HandleFunc(method string, path string, handler http.HandlerFunc) {
	router.Handle(method, path, handler)
}

func (router *Router) Handle(method string, path string, handler http.Handler) {
	if router.prefix != "" {
		path = router.prefix + path
	}

	handler = router.applyMiddleware(handler)

	router.tree.addRoute(method, path, handler)
}

func (router *Router) Get(path string, handler http.HandlerFunc) {
	router.HandleFunc(http.MethodGet, path, handler)
}

func (router *Router) Post(path string, handler http.HandlerFunc) {
	router.HandleFunc(http.MethodPost, path, handler)
}

func (router *Router) Put(path string, handler http.HandlerFunc) {
	router.HandleFunc(http.MethodPut, path, handler)
}

func (router *Router) Delete(path string, handler http.HandlerFunc) {
	router.HandleFunc(http.MethodDelete, path, handler)
}

func (router *Router) Patch(path string, handler http.HandlerFunc) {
	router.HandleFunc(http.MethodPatch, path, handler)
}

func (router *Router) getRouteMethods(r *http.Request) string {
	cleaned := cleanPath(r.URL.Path)

	node, _, found := router.tree.find(cleaned)
	if !found || len(node.handlers) == 0 {
		return ""
	}

	methods := make([]string, 0, len(node.handlers))
	for method := range node.handlers {
		methods = append(methods, method)
	}

	slices.Sort(methods)
	return strings.Join(methods, ",")
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	cleaned := cleanPath(r.URL.Path)

	node, params, found := router.tree.find(cleaned)
	if !found || len(node.handlers) == 0 {
		router.notFoundHandler.ServeHTTP(w, r)
		return
	}

	handler, exists := node.handlers[r.Method]
	if !exists {
		w.Header().Set("Allow", router.getRouteMethods(r))

		if r.Method == http.MethodOptions {
			router.optionsHandler.ServeHTTP(w, r)
			return
		}
		router.methodNotAllowedHandler.ServeHTTP(w, r)
		return
	}

	r = setPathParams(r, params)
	handler.ServeHTTP(w, r)
}

func cleanPath(p string) string {
	if p == "" {
		return "/"
	}

	cleaned := path.Clean(p)
	if cleaned == "." {
		return "/"
	}

	return cleaned
}

var knownPatterns = map[string]string{
	"<int>":    `\d+`,
	"<uuid>":   `[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}|[0-9a-fA-F]{32}`,
	"<string>": `[^/]+`,
}

func parsePattern(path string) (string, *regexp.Regexp, error) {
	if strings.HasPrefix(path, `{`) && strings.HasSuffix(path, `}`) {
		if len(path) < 3 {
			return "", nil, errors.New("mux: patterned routes cannot be empty")
		}

		split := strings.SplitN(path[1:len(path)-1], ":", 2)
		if len(split[0]) == 0 {
			return "", nil, errors.New("mux: patterned routes must have a parameter name")
		}

		if len(split) == 1 {
			return split[0], nil, nil
		}

		param, pattern := split[0], split[1]

		if knownPattern, ok := knownPatterns[pattern]; ok {
			pattern = knownPattern
		}

		anchored := fmt.Sprintf("^%s$", pattern)
		re := regexp.MustCompile(anchored)
		return param, re, nil
	}

	if strings.Contains(path, "{") || strings.Contains(path, "}") {
		return "", nil, errors.New("mux: unbalanced braces in patterned route")
	}

	return "", nil, nil
}
