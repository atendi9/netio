package netio

import (
	"bytes"
	"net"
	"strings"
	"testing"
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

	if !strings.HasPrefix(got, "HTTP/1.1 200 OK") {
		t.Errorf("esperado status 'HTTP/1.1 200 OK', got: %s", got)
	}
	if !strings.Contains(got, "X-Test-Header: TestValue") {
		t.Errorf("esperado header 'X-Test-Header: TestValue', got: %s", got)
	}
	if !strings.Contains(got, "Content-Length: 13") {
		t.Errorf("esperado Content-Length 13, got: %s", got)
	}
	if !strings.HasSuffix(got, "Hello, world!") {
		t.Errorf("esperado body 'Hello, world!', got: %s", got)
	}
}
