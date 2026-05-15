package netio

import "bytes"

type node struct {
	part     []byte
	children []*node
	param    bool
	key      []byte
	handlers map[string][]Handler
}

func (n *node) addMethod(method string, path [][]byte, h []Handler) {
	if n.handlers == nil {
		n.handlers = make(map[string][]Handler)
	}

	if len(path) == 0 {
		n.handlers[method] = h
		return
	}

	part := path[0]
	isParam := len(part) > 0 && part[0] == ':'

	for _, c := range n.children {
		if bytes.Equal(c.part, part) {
			c.addMethod(method, path[1:], h)
			return
		}
	}

	// Reuse existing param node if adding another param at same level
	if isParam {
		for _, c := range n.children {
			if c.param {
				c.addMethod(method, path[1:], h)
				return
			}
		}
	}

	child := &node{
		part:  part,
		param: isParam,
	}
	if isParam {
		child.key = part[1:]
	}

	n.children = append(n.children, child)
	child.addMethod(method, path[1:], h)
}

func (n *node) findMethod(method string, path [][]byte, params *[]KV) ([]Handler, bool) {
	if len(path) == 0 {
		h, ok := n.handlers[method]
		return h, ok
	}

	// First try the exact static child. If the recursive descent dead-ends,
	// fall through and also try a param sibling — without backtracking, a
	// static segment that matches at one depth but fails deeper would shadow
	// an otherwise-matching param route registered at the same level.
	for _, c := range n.children {
		if !c.param && bytes.Equal(c.part, path[0]) {
			if h, ok := c.findMethod(method, path[1:], params); ok {
				return h, true
			}
			break
		}
	}

	// Try the param child, trimming any params appended on an abandoned branch.
	for _, c := range n.children {
		if c.param {
			mark := len(*params)
			*params = append(*params, KV{c.key, path[0]})
			if h, ok := c.findMethod(method, path[1:], params); ok {
				return h, true
			}
			*params = (*params)[:mark]
		}
	}

	return nil, false
}
