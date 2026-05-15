package netio

import (
	"testing"
)

func TestGroup_Post(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.Group("/api").Post("/users", func(c *Context) {})
	if _, ok := app.root.findMethod("POST", split("/api/users"), &[]KV{}); !ok {
		t.Error("POST route not registered")
	}
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