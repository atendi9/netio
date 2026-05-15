package netio

import (
	"bytes"
	"testing"
)

func TestNode_StaticPath(t *testing.T) {
	root := &node{}
	root.addMethod("GET", [][]byte{[]byte("home")}, []Handler{func(c *Context) {}})

	params := []KV{}
	handlers, ok := root.findMethod("GET", [][]byte{[]byte("home")}, &params)
	if !ok || len(handlers) != 1 || len(params) != 0 {
		t.Errorf("expected handler with no params, got ok=%v params=%v", ok, params)
	}
}

func TestNode_ParamPath(t *testing.T) {
	root := &node{}
	root.addMethod("GET", [][]byte{[]byte("user"), []byte(":id")}, []Handler{func(c *Context) {}})

	params := []KV{}
	handlers, ok := root.findMethod("GET", [][]byte{[]byte("user"), []byte("42")}, &params)
	if !ok || len(handlers) != 1 {
		t.Fatal("expected handler found")
	}
	if len(params) != 1 || !bytes.Equal(params[0].K, []byte("id")) || !bytes.Equal(params[0].V, []byte("42")) {
		t.Errorf("unexpected params: %+v", params)
	}
}

func TestNode_NotFound(t *testing.T) {
	root := &node{}
	root.addMethod("GET", [][]byte{[]byte("home")}, []Handler{func(c *Context) {}})

	params := []KV{}
	h, ok := root.findMethod("GET", [][]byte{[]byte("unknown")}, &params)
	if ok || h != nil {
		t.Error("expected no handler found")
	}
}

func TestNode_StaticPreferredOverParam(t *testing.T) {
	root := &node{}
	root.addMethod("GET", [][]byte{[]byte("users"), []byte(":id")}, []Handler{func(c *Context) {}})
	root.addMethod("GET", [][]byte{[]byte("users"), []byte("count")}, []Handler{func(c *Context) {}})

	// Static "count" should match, not param ":id"
	params := []KV{}
	_, ok := root.findMethod("GET", [][]byte{[]byte("users"), []byte("count")}, &params)
	if !ok {
		t.Fatal("expected static route to match")
	}
	if len(params) != 0 {
		t.Errorf("expected no params for static match, got %+v", params)
	}

	// Other values should still match param
	params = []KV{}
	_, ok = root.findMethod("GET", [][]byte{[]byte("users"), []byte("42")}, &params)
	if !ok {
		t.Fatal("expected param route to match")
	}
	if len(params) != 1 || !bytes.Equal(params[0].K, []byte("id")) {
		t.Errorf("expected param 'id', got %+v", params)
	}
}

func TestNode_ParamReuseOnAdd(t *testing.T) {
	root := &node{}
	// Register two different param routes at the same level — should reuse the param node
	root.addMethod("GET", [][]byte{[]byte("items"), []byte(":id")}, []Handler{func(c *Context) {}})
	root.addMethod("POST", [][]byte{[]byte("items"), []byte(":name")}, []Handler{func(c *Context) {}})

	// Should have only one child under "items" (reused param node)
	itemsNode := root.children[0]
	if len(itemsNode.children) != 1 {
		t.Errorf("expected 1 child (param node reused), got %d", len(itemsNode.children))
	}
}

// TestNode_BacktrackToParamSibling guards the findMethod backtracking bug:
// when a static segment matches at depth N but the route dead-ends deeper, a
// param sibling at depth N that would match the full path must still be tried.
func TestNode_BacktrackToParamSibling(t *testing.T) {
	root := &node{}
	// Register the static branch first so it is tried first during lookup.
	root.addMethod("GET", [][]byte{[]byte("users"), []byte("new")}, []Handler{func(c *Context) {}})
	root.addMethod("GET", [][]byte{[]byte("users"), []byte(":id"), []byte("posts")}, []Handler{func(c *Context) {}})

	// "users/new" matches the static segment but dead-ends (no "posts" child);
	// the param route "users/:id/posts" must still be reachable.
	params := []KV{}
	h, ok := root.findMethod("GET", [][]byte{[]byte("users"), []byte("new"), []byte("posts")}, &params)
	if !ok || h == nil {
		t.Fatal("expected param route to match via backtracking")
	}
	if len(params) != 1 || !bytes.Equal(params[0].K, []byte("id")) || !bytes.Equal(params[0].V, []byte("new")) {
		t.Errorf("expected param id=new, got %+v", params)
	}

	// The plain static route must still resolve directly.
	params = []KV{}
	if _, ok := root.findMethod("GET", [][]byte{[]byte("users"), []byte("new")}, &params); !ok {
		t.Error("expected static route /users/new to still match")
	}
	if len(params) != 0 {
		t.Errorf("expected no params for static match, got %+v", params)
	}
}

// TestNode_BacktrackTrimsParams ensures params appended on an abandoned static
// branch's failed sibling attempts do not leak into the final result.
func TestNode_BacktrackTrimsParams(t *testing.T) {
	root := &node{}
	root.addMethod("GET", [][]byte{[]byte("a"), []byte(":x"), []byte("b")}, []Handler{func(c *Context) {}})

	// No route matches a/v/c — params must come back empty, not carry ":x".
	params := []KV{}
	if _, ok := root.findMethod("GET", [][]byte{[]byte("a"), []byte("v"), []byte("c")}, &params); ok {
		t.Fatal("expected no match for a/v/c")
	}
	if len(params) != 0 {
		t.Errorf("expected params trimmed on failed branch, got %+v", params)
	}
}

func TestNode_DifferentMethodSamePath(t *testing.T) {
	root := &node{}
	root.addMethod("GET", [][]byte{[]byte("user"), []byte(":id")}, []Handler{func(c *Context) {}})
	root.addMethod("POST", [][]byte{[]byte("user"), []byte(":id")}, []Handler{func(c *Context) {}})

	params := []KV{}
	handlers, ok := root.findMethod("POST", [][]byte{[]byte("user"), []byte("42")}, &params)
	if !ok || len(handlers) != 1 {
		t.Fatal("expected POST handler found")
	}
	if len(params) != 1 || !bytes.Equal(params[0].K, []byte("id")) || !bytes.Equal(params[0].V, []byte("42")) {
		t.Errorf("unexpected params: %+v", params)
	}
}