package netio

import (
	"net"
	"testing"
)

func TestGroupPrefix(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})

	var called bool

	g := app.Group("/api")
	g.Get("/users", func(c *Context) {
		called = true
		c.JSON(map[string]any{"message": "Hello World"})
	})

	params := []KV{}
	h, ok := app.root.findMethod("GET", split("/api/users"), &params)
	if !ok {
		t.Fatalf("expected route to be registered")
	}

	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	ctx := &Context{}
	ctx.reset()
	ctx.conn = server
	ctx.handlers = h

	done := make(chan struct{})

	go func() {
		ctx.Next()
		server.Close()
		close(done)
	}()

	buf := make([]byte, 1024)
	_, _ = client.Read(buf)

	<-done

	if !called {
		t.Fatalf("handler was not called")
	}
}
