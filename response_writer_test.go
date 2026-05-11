package netio

import (
	"bytes"
	"net"
	"strings"
	"testing"

	"github.com/atendi9/capivara/assert"
)

func TestWriteResponseWithHeaders(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	status := 200
	body := []byte("Hello, world!")
	headers := []KV{
		{K: []byte("X-Test-Header"), V: []byte("TestValue")},
	}

	go func() {
		ctx := &Context{
			conn:      server,
			resHeader: headers,
		}
		ctx.Status(status).Send(body)
	}()

	var buf bytes.Buffer
	tmp := make([]byte, 1024)
	n, _ := client.Read(tmp)
	buf.Write(tmp[:n])

	got := buf.String()
	assert.True(t, strings.HasPrefix(got, "HTTP/1.1 200 OK"))
	assert.True(t, strings.Contains(got, "X-Test-Header: TestValue"))
	assert.True(t, strings.Contains(got, "Content-Length: 13"))
	assert.True(t, strings.HasSuffix(got, "Hello, world!"))
}
