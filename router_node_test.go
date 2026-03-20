package netio

import (
	"bytes"
	"testing"
)

func TestNode(t *testing.T) {
	root := &node{}

	// Handlers fictícios
	handler := func(c *Context) {
		c.Send([]byte(c.Path()))
	}
	hHome := []Handler{handler}
	hUser := []Handler{handler}
	hPost := []Handler{handler}

	// Adicionando métodos
	root.addMethod("GET", [][]byte{[]byte("home")}, hHome)
	root.addMethod("GET", [][]byte{[]byte("user"), []byte(":id")}, hUser)
	root.addMethod("POST", [][]byte{[]byte("user"), []byte(":id")}, hPost)

	t.Run("find existing static path", func(t *testing.T) {
		params := []KV{}
		handlers, ok := root.findMethod("GET", [][]byte{[]byte("home")}, &params)
		if !ok {
			t.Fatal("expected handler found")
		}
		if len(handlers) != 1 {
			t.Fail()
		}
		if len(params) != 0 {
			t.Fatalf("expected no params, got %+v", params)
		}
	})

	t.Run("find existing param path", func(t *testing.T) {
		params := []KV{}
		handlers, ok := root.findMethod("GET", [][]byte{[]byte("user"), []byte("42")}, &params)
		if !ok {
			t.Fatal("expected handler found")
		}
		if len(handlers) != 1 {
			t.Fail()
		}
		if len(params) != 1 || !bytes.Equal(params[0].K, []byte("id")) || !bytes.Equal(params[0].V, []byte("42")) {
			t.Fatalf("unexpected params: %+v", params)
		}
	})

	t.Run("find non-existing path", func(t *testing.T) {
		params := []KV{}
		h, ok := root.findMethod("GET", [][]byte{[]byte("unknown")}, &params)
		if ok || h != nil {
			t.Fatal("expected no handler found")
		}
	})

	t.Run("different method on same path", func(t *testing.T) {
		params := []KV{}
		handlers, ok := root.findMethod("POST", [][]byte{[]byte("user"), []byte("42")}, &params)
		if !ok {
			t.Fatal("expected handler found")
		}
		if len(handlers) != 1 {
			t.Fail()
		}
		if len(params) != 1 || !bytes.Equal(params[0].K, []byte("id")) || !bytes.Equal(params[0].V, []byte("42")) {
			t.Fatalf("unexpected params: %+v", params)
		}
	})
}
