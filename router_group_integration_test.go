//go:build integration

package netio

import (
	"net"
	"testing"
)

func TestGroup_Get(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	called := false
	app.Group("/api").Get("/users", func(c *Context) { called = true })

	params := []KV{}
	h, ok := app.root.findMethod("GET", split("/api/users"), &params)
	if !ok {
		t.Fatal("expected route to be registered")
	}

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

	client.Read(make([]byte, 1024))
	<-done

	if !called {
		t.Error("handler was not called")
	}
}
