package netio

// Handler defines the signature for request handler functions
// that process a Context.
type Handler func(c *Context)
