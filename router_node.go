package netio

import "bytes"

type node struct {
	part     []byte
	children []*node
	param    bool
	handlers map[string]*route
}

// route is the handler chain registered for one method on a node, kept together
// with the pattern it was registered under and the parameter names that pattern
// spells.
//
// The names live here rather than on the node because a node matches any
// segment while the name is the route's own: two routes may name the same
// position differently ("/:gmail/:id" alongside "/:enterprise/all"), and a name
// stored on the shared node would hand every later route the first one's.
//
// The pattern serves a second purpose: it is what a metric or a log line should
// be keyed by, since the concrete path carries values a client picks freely.
type route struct {
	handlers  []Handler
	pattern   string
	paramKeys [][]byte
}

func (n *node) addMethod(method, pattern string, path [][]byte, h []Handler) {
	n.addRoute(method, pattern, path, h, nil)
}

// addRoute walks the pattern's segments, collecting the parameter names it
// passes so the leaf can record them in the order a match will produce them.
func (n *node) addRoute(method, pattern string, path [][]byte, h []Handler, keys [][]byte) {
	if n.handlers == nil {
		n.handlers = make(map[string]*route)
	}

	if len(path) == 0 {
		n.handlers[method] = &route{handlers: h, pattern: pattern, paramKeys: keys}
		return
	}

	part := path[0]
	isParam := len(part) > 0 && part[0] == ':'
	if isParam {
		keys = append(keys, part[1:])
	}

	for _, c := range n.children {
		if bytes.Equal(c.part, part) {
			c.addRoute(method, pattern, path[1:], h, keys)
			return
		}
	}

	// Reuse existing param node if adding another param at same level
	if isParam {
		for _, c := range n.children {
			if c.param {
				c.addRoute(method, pattern, path[1:], h, keys)
				return
			}
		}
	}

	child := &node{
		part:  part,
		param: isParam,
	}

	n.children = append(n.children, child)
	child.addRoute(method, pattern, path[1:], h, keys)
}

func (n *node) findMethod(method string, path [][]byte, params *[]KV) (*route, bool) {
	if len(path) == 0 {
		r, ok := n.handlers[method]
		return r, ok
	}

	// First try the exact static child. If the recursive descent dead-ends,
	// fall through and also try a param sibling — without backtracking, a
	// static segment that matches at one depth but fails deeper would shadow
	// an otherwise-matching param route registered at the same level.
	for _, c := range n.children {
		if !c.param && bytes.Equal(c.part, path[0]) {
			if r, ok := c.findMethod(method, path[1:], params); ok {
				return r, true
			}
			break
		}
	}

	// Try the param child, trimming any params appended on an abandoned branch.
	// Only the value is recorded: the name comes from the route that ends up
	// matching, which the caller fills in once the walk succeeds.
	for _, c := range n.children {
		if c.param {
			mark := len(*params)
			*params = append(*params, KV{V: path[0]})
			if r, ok := c.findMethod(method, path[1:], params); ok {
				return r, true
			}
			*params = (*params)[:mark]
		}
	}

	return nil, false
}
