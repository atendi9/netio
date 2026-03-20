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
		if bytes.Equal(c.part, part) || c.param {
			c.addMethod(method, path[1:], h)
			return
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

	for _, c := range n.children {
		if bytes.Equal(c.part, path[0]) || c.param {
			if c.param {
				*params = append(*params, KV{c.key, path[0]})
			}
			return c.findMethod(method, path[1:], params)
		}
	}

	return nil, false
}
