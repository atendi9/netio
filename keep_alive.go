package netio

import "bytes"

func keepAlive(c *Context) bool {
	v := header(c, []byte("Connection"))
	return !bytes.Equal(v, []byte("close"))
}
