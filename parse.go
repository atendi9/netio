package netio

import (
	"bufio"
	"bytes"
	"strconv"
)

func parseRequest(r *bufio.Reader, c *Context) bool {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return false
	}

	parseRequestLine(line, c)
	parseHeaders(r, c)
	parseBody(r, c)

	return true
}

func parseRequestLine(line []byte, c *Context) {
	i := bytes.IndexByte(line, ' ')
	c.method = line[:i]

	j := bytes.IndexByte(line[i+1:], ' ')
	uri := line[i+1 : i+1+j]

	if q := bytes.IndexByte(uri, '?'); q != -1 {
		c.path = uri[:q]
		parseQueryString(uri[q+1:], c)
	} else {
		c.path = uri
	}
}

func parseHeaders(r *bufio.Reader, c *Context) {
	for {
		line, _ := r.ReadBytes('\n')
		if len(line) == 2 {
			return
		}

		i := bytes.IndexByte(line, ':')
		k := line[:i]
		v := bytes.TrimSpace(line[i+1:])

		c.header = append(c.header, KV{k, v})
	}
}

func parseQueryString(qs []byte, c *Context) {
	for len(qs) > 0 {
		var pair []byte
		if i := bytes.IndexByte(qs, '&'); i != -1 {
			pair = qs[:i]
			qs = qs[i+1:]
		} else {
			pair = qs
			qs = nil
		}
		if len(pair) == 0 {
			continue
		}
		if eq := bytes.IndexByte(pair, '='); eq != -1 {
			c.query = append(c.query, KV{pair[:eq], pair[eq+1:]})
		} else {
			c.query = append(c.query, KV{pair, nil})
		}
	}
}

func parseBody(r *bufio.Reader, c *Context) {
	if cl := header(c, []byte("Content-Length")); cl != nil {
		n := atoi(cl)
		buf := make([]byte, n)
		r.Read(buf)
		c.body = buf
		return
	}

	if te := header(c, []byte("Transfer-Encoding")); bytes.Equal(te, []byte("chunked")) {
		parseChunked(r, c)
	}
}

func atoi(b []byte) int {
	n := 0
	for _, v := range b {
		n = n*10 + int(v-'0')
	}
	return n
}

func parseChunked(r *bufio.Reader, c *Context) {
	var body []byte

	for {
		line, _ := r.ReadBytes('\n')
		size, _ := strconv.ParseInt(string(bytes.TrimSpace(line)), 16, 64)

		if size == 0 {
			r.ReadBytes('\n')
			break
		}

		chunk := make([]byte, size)
		r.Read(chunk)
		body = append(body, chunk...)
		r.ReadBytes('\n')
	}

	c.body = body
}