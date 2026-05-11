package netio

import (
	"bytes"
	"testing"

	"github.com/atendi9/capivara/assert"
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
		assert.True(t, ok)
		assert.LengthSlice(t, 1, handlers)
		assert.LengthSlice(t, 0, params)
	})

	t.Run("find existing param path", func(t *testing.T) {
		params := []KV{}
		handlers, ok := root.findMethod("GET", [][]byte{[]byte("user"), []byte("42")}, &params)
		assert.True(t, ok)
		assert.LengthSlice(t, 1 ,handlers)
		ok = len(params) != 1 || !bytes.Equal(params[0].K, []byte("id")) || !bytes.Equal(params[0].V, []byte("42"))
		assert.False(t, ok)
	})

	t.Run("find non-existing path", func(t *testing.T) {
		params := []KV{}
		h, ok := root.findMethod("GET", [][]byte{[]byte("unknown")}, &params)
		assert.False(t, ok || h != nil)
	})

	t.Run("different method on same path", func(t *testing.T) {
		params := []KV{}
		handlers, ok := root.findMethod("POST", [][]byte{[]byte("user"), []byte("42")}, &params)
		assert.True(t, ok)
		assert.LengthSlice(t, 1, handlers)
		ok = len(params) != 1 || !bytes.Equal(params[0].K, []byte("id")) || !bytes.Equal(params[0].V, []byte("42"))
		assert.False(t, ok)
	})
}
