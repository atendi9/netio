package netio

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

	// Group creates a new Router with the given path prefix and optional middleware.
	//
	// The returned Router inherits the current base path and middleware stack,
	// allowing nested groups for better route organization.
	Group(path string, m ...Handler) Router
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

func (g *group) Use(h Handler) {
	g.middlewares = append(g.middlewares, h)
}

func (g *group) join(path string) string {
	if path == "" {
		return g.basePath
	}
	return g.basePath + path
}

func (g *group) chain(h []Handler) []Handler {
	return append(g.middlewares, h...)
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

func (g *group) Group(path string, m ...Handler) Router {
	return &group{
		app:         g.app,
		basePath:    g.join(path),
		middlewares: append(g.middlewares, m...),
	}
}
