package netio

import "strings"

// Router defines a contract for registering HTTP routes and creating route groups.
//
// A Router allows attaching handlers to specific HTTP methods and paths.
// It also supports grouping routes under a common path prefix with shared middleware.
//
// Implementations should ensure that middlewares defined in groups
// are executed before route-specific handlers.
type Router interface {
	Get(path string, h ...Handler)
	Post(path string, h ...Handler)
	Put(path string, h ...Handler)
	Delete(path string, h ...Handler)
	Patch(path string, h ...Handler)

	// Query registers a handler for the QUERY method (RFC 10008), whose query
	// travels in the request content instead of the URI.
	Query(path string, h ...Handler)

	// Group creates a new Router with the given path prefix and optional middleware.
	//
	// The returned Router inherits the current base path and middleware stack,
	// allowing nested groups for better route organization.
	Group(path string, m ...Handler) Router
	Use(h ...Handler)
}

type group struct {
	app         *App
	basePath    string
	middlewares []Handler
}

// Group creates a new route group with a common base path and middleware.
//
// All routes registered within this group will be prefixed with basePath,
// and the provided middlewares will be executed before the route handlers.
//
// Groups can be nested, and child groups inherit both the path prefix
// and middleware stack from their parent.
func (a *App) Group(basePath string, m ...Handler) Router {
	return &group{
		app:         a,
		basePath:    basePath,
		middlewares: m,
	}
}

func (g *group) Use(middlewares ...Handler) {
	g.middlewares = append(g.middlewares, middlewares...)
}

// join builds the full path of a route registered on the group. A route path of
// "" or "/" addresses the group's own path, and the slash between base and path
// is emitted exactly once: concatenating them raw turned Get("/") into "/v1/"
// and a base ending in "/" into "/v1//users", neither of which the router could
// match against an incoming "/v1" or "/v1/users".
func (g *group) join(path string) string {
	base := strings.TrimRight(g.basePath, "/")
	path = strings.TrimRight(path, "/")

	if path != "" && path[0] != '/' {
		path = "/" + path
	}

	if joined := base + path; joined != "" {
		return joined
	}

	return "/"
}

func (g *group) chain(h []Handler) []Handler {
	all := make([]Handler, 0, len(g.middlewares)+len(h))
	all = append(all, g.middlewares...)
	all = append(all, h...)
	return all
}

func (g *group) Get(path string, h ...Handler) {
	g.app.GET(g.join(path), g.chain(h)...)
}

func (g *group) Post(path string, h ...Handler) {
	g.app.POST(g.join(path), g.chain(h)...)
}

func (g *group) Put(path string, h ...Handler) {
	g.app.PUT(g.join(path), g.chain(h)...)
}

func (g *group) Delete(path string, h ...Handler) {
	g.app.DELETE(g.join(path), g.chain(h)...)
}

func (g *group) Patch(path string, h ...Handler) {
	g.app.PATCH(g.join(path), g.chain(h)...)
}

func (g *group) Query(path string, h ...Handler) {
	// The guard is inserted inside the chain rather than by App.QUERY, so the
	// group's middlewares run ahead of it and a request rejected for a missing
	// Content-Type still carries whatever headers they set.
	g.app.queryRoute(g.join(path), g.chain(guardContentType(h)))
}

func (g *group) Group(path string, m ...Handler) Router {
	mws := make([]Handler, len(g.middlewares), len(g.middlewares)+len(m))
	copy(mws, g.middlewares)
	mws = append(mws, m...)
	return &group{
		app:         g.app,
		basePath:    g.join(path),
		middlewares: mws,
	}
}
