package netio

import (
	"net"
	"strings"
	"testing"

	"github.com/atendi9/capivara/assert"
)

func TestGroup_Post(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.Group("/api").Post("/users", func(c *Context) {})
	if _, ok := app.root.findMethod("POST", split("/api/users"), &[]KV{}); !ok {
		t.Error("POST route not registered")
	}
}

func TestGroup_Execution(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})

	var called bool

	g := app.Group("/api")
	g.Get("/users", func(c *Context) {
		called = true
		c.JSON(map[string]any{"message": "Hello World"})
	})

	params := []KV{}
	h, ok := app.root.findMethod("GET", split("/api/users"), &params)
	assert.True(t, ok)

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	ctx := &Context{}
	ctx.reset()
	ctx.conn = server
	ctx.handlers = h.handlers

	done := make(chan struct{})

	go func() {
		ctx.Next()
		server.Close()
		close(done)
	}()

	buf := make([]byte, 1024)
	_, _ = client.Read(buf)

	<-done
	assert.True(t, called)
}

func TestGroup_Put(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.Group("/api").Put("/users", func(c *Context) {})
	if _, ok := app.root.findMethod("PUT", split("/api/users"), &[]KV{}); !ok {
		t.Error("PUT route not registered")
	}
}

func TestGroup_Delete(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.Group("/api").Delete("/users", func(c *Context) {})
	if _, ok := app.root.findMethod("DELETE", split("/api/users"), &[]KV{}); !ok {
		t.Error("DELETE route not registered")
	}
}

func TestGroup_Patch(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.Group("/api").Patch("/users", func(c *Context) {})
	if _, ok := app.root.findMethod("PATCH", split("/api/users"), &[]KV{}); !ok {
		t.Error("PATCH route not registered")
	}
}

func TestGroup_Use(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	g := app.Group("/api").(*group)
	g.Use(func(c *Context) {})
	if len(g.middlewares) != 1 {
		t.Errorf("expected 1 middleware, got %d", len(g.middlewares))
	}
}

func TestGroup_NestedGroup(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.Group("/v1").Group("/users").Get("/:id", func(c *Context) {})

	params := make([]KV, 0, 8)
	if _, ok := app.root.findMethod("GET", split("/v1/users/123"), &params); !ok {
		t.Error("nested group route not registered")
	}
}

func TestGroup_JoinEmptyPath(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	g := app.Group("/base").(*group)
	if got := g.join(""); got != "/base" {
		t.Errorf("expected /base, got %q", got)
	}
}

// join used to concatenate base and path raw, so Get("/") registered "/base/"
// and a base written with a trailing slash registered "/base//users" — paths no
// incoming request matches.
func TestGroup_JoinNormalizesSlashes(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})

	tests := []struct {
		base string
		path string
		want string
	}{
		{"/base", "/", "/base"},
		{"/base", "", "/base"},
		{"/base", "/users", "/base/users"},
		{"/base", "users", "/base/users"},
		{"/base", "/users/", "/base/users"},
		{"/base/", "/users", "/base/users"},
		{"/base/", "/", "/base"},
		{"/", "/", "/"},
		{"/", "/users", "/users"},
		{"", "/users", "/users"},
		{"", "/", "/"},
	}

	for _, tt := range tests {
		g := app.Group(tt.base).(*group)
		if got := g.join(tt.path); got != tt.want {
			t.Errorf("Group(%q).join(%q) = %q, want %q", tt.base, tt.path, got, tt.want)
		}
	}
}

// A group whose route is registered as "/" answers the group's own path: this
// is how a collection endpoint ("GET /v1/budget") is written.
func TestGroup_RootPathRoutes(t *testing.T) {
	methods := []struct {
		name     string
		register func(Router, ...Handler)
	}{
		{"GET", func(r Router, h ...Handler) { r.Get("/", h...) }},
		{"POST", func(r Router, h ...Handler) { r.Post("/", h...) }},
		{"PUT", func(r Router, h ...Handler) { r.Put("/", h...) }},
		{"DELETE", func(r Router, h ...Handler) { r.Delete("/", h...) }},
		{"PATCH", func(r Router, h ...Handler) { r.Patch("/", h...) }},
		{"QUERY", func(r Router, h ...Handler) { r.Query("/", h...) }},
	}

	for _, m := range methods {
		t.Run(m.name, func(t *testing.T) {
			app, _ := New(AppConfig{Port: "0"})
			m.register(app.Group("/v1/budget"), func(c *Context) {})

			for _, path := range []string{"/v1/budget", "/v1/budget/"} {
				params := []KV{}
				if _, ok := app.root.findMethod(m.name, split(path), &params); !ok {
					t.Errorf("%s %s not registered", m.name, path)
				}
			}
		})
	}
}

// The nested equivalent: Group("/v1").Group("/budget").Get("/").
func TestGroup_NestedRootPathRoute(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.Group("/v1").Group("/budget").Get("/", func(c *Context) {})

	params := []KV{}
	if _, ok := app.root.findMethod("GET", split("/v1/budget"), &params); !ok {
		t.Error("nested group root route not registered")
	}
}

// Registering the group's own path must not disturb the routes hanging off it.
func TestGroup_RootPathDoesNotShadowSiblings(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})

	g := app.Group("/v1/budget")
	g.Get("/", func(c *Context) {})
	g.Get("/:id", func(c *Context) {})

	params := []KV{}
	if _, ok := app.root.findMethod("GET", split("/v1/budget/42"), &params); !ok {
		t.Fatal("param route not registered alongside the root route")
	}
	if len(params) != 1 || string(params[0].V) != "42" {
		t.Errorf("params = %v, want id=42", params)
	}
}

// Middlewares must reach the root route like any other: the CORS middleware a
// group carries is what makes the browser accept the response.
func TestGroup_RootPathRunsMiddlewares(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})

	var order []string
	g := app.Group("/v1", func(c *Context) {
		order = append(order, "mw")
		c.Next()
	})
	g.Get("/", func(c *Context) { order = append(order, "handler") })

	params := []KV{}
	h, ok := app.root.findMethod("GET", split("/v1"), &params)
	if !ok {
		t.Fatal("root route not registered")
	}

	ctx := &Context{}
	ctx.reset()
	ctx.handlers = h.handlers
	ctx.Next()

	if len(order) != 2 || order[0] != "mw" || order[1] != "handler" {
		t.Errorf("execution order = %v, want [mw handler]", order)
	}
}

// A route registered without the trailing slash still answers a request that
// carries one, matching how the app was reachable under Fiber.
func TestApp_TrailingSlashRequestMatchesRoute(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.GET("/v1/dashboard", func(c *Context) {})

	params := []KV{}
	if _, ok := app.lookup("GET", []byte("/v1/dashboard/"), &params); !ok {
		t.Error("request with a trailing slash did not match the route")
	}
}

// ...and the reverse: a route registered with one answers a request without it.
func TestApp_TrailingSlashRouteMatchesRequest(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.GET("/v1/dashboard/", func(c *Context) {})

	params := []KV{}
	if _, ok := app.lookup("GET", []byte("/v1/dashboard"), &params); !ok {
		t.Error("route registered with a trailing slash did not match the request")
	}
}

// A group's collection endpoint is registered as Get("/"), which used to build
// "/v1/budget/" — a path carrying an empty trailing segment that no request
// could match, so the route 404'd while every sibling under it worked.
func TestServe_GroupRootRouteIsReachable(t *testing.T) {
	requests := []string{
		"GET /v1/budget HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n",
		"GET /v1/budget/ HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n",
	}

	for _, raw := range requests {
		app, _ := New(AppConfig{Port: "0"})
		app.Group("/v1/budget").Get("/", func(c *Context) { c.Send([]byte("collection")) })

		resp := serveRequest(t, app, raw)
		if !strings.Contains(resp, "200 OK") || !strings.Contains(resp, "collection") {
			t.Errorf("%q answered:\n%s", strings.SplitN(raw, "\r\n", 2)[0], resp)
		}
	}
}

// Two routes may name the same position differently — "/:gmail/:budget_number"
// registered alongside "/:enterprise_name/all". The name belongs to the route,
// not to the trie node they share: storing it on the node handed every later
// route the first one's name, and a handler asking for its own parameter got an
// empty string while the value sat under a name it never heard of.
func TestGroup_ParamNamesAreRouteLocal(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})

	g := app.Group("/v1/account/budget")
	g.Get("/:enterprise_name/all", func(c *Context) {})
	g.Get("/:gmail/:budget_number", func(c *Context) {})
	g.Get("/:gmail/search/:prefix", func(c *Context) {})

	tests := []struct {
		path string
		want map[string]string
	}{
		{"/v1/account/budget/Acme/all", map[string]string{"enterprise_name": "Acme"}},
		{"/v1/account/budget/test@test.com/42", map[string]string{"gmail": "test@test.com", "budget_number": "42"}},
		{"/v1/account/budget/test@test.com/search/or", map[string]string{"gmail": "test@test.com", "prefix": "or"}},
	}

	for _, tt := range tests {
		params := make([]KV, 0, 8)
		if _, ok := app.lookup("GET", []byte(tt.path), &params); !ok {
			t.Fatalf("%s did not match any route", tt.path)
		}

		got := map[string]string{}
		for _, kv := range params {
			got[string(kv.K)] = string(kv.V)
		}
		if len(got) != len(tt.want) {
			t.Errorf("%s params = %v, want %v", tt.path, got, tt.want)
		}
		for k, want := range tt.want {
			if got[k] != want {
				t.Errorf("%s Params(%q) = %q, want %q", tt.path, k, got[k], want)
			}
		}
	}
}

// The same route-local naming when the colliding registrations come from
// different groups hanging off one prefix, which is how the account routes are
// written: Group("/:id") and Group("/:gmail") share a parent.
func TestGroup_ParamNamesAcrossSiblingGroups(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})

	account := app.Group("/v1/account/whatsapp")
	account.Group("/:id").Post("/message/send", func(c *Context) {})
	account.Group("/:gmail").Get("/", func(c *Context) {})

	params := make([]KV, 0, 8)
	if _, ok := app.lookup("GET", []byte("/v1/account/whatsapp/test@test.com"), &params); !ok {
		t.Fatal("the /:gmail group route did not match")
	}
	if len(params) != 1 || string(params[0].K) != "gmail" || string(params[0].V) != "test@test.com" {
		t.Errorf("params = %v, want gmail=test@test.com", params)
	}
}

// A HEAD request falling back to the GET route is named by that route too.
func TestApp_ParamNamesOnHeadFallback(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.GET("/v1/file/:fileId", func(c *Context) {})
	app.GET("/v1/:gmail/all", func(c *Context) {})

	params := make([]KV, 0, 8)
	if _, ok := app.lookup("HEAD", []byte("/v1/file/7"), &params); !ok {
		t.Fatal("HEAD did not fall back to the GET route")
	}
	if len(params) != 1 || string(params[0].K) != "fileId" || string(params[0].V) != "7" {
		t.Errorf("params = %v, want fileId=7", params)
	}
}
