package netio

import (
	"net"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteResponseWithHeaders(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		ctx := &Context{
			conn:      server,
			resHeader: []KV{{K: []byte("X-Test-Header"), V: []byte("TestValue")}},
		}
		ctx.Status(200).Send([]byte("Hello, world!"))
	}()

	buf := make([]byte, 1024)
	n, _ := client.Read(buf)
	got := string(buf[:n])

	if !strings.HasPrefix(got, "HTTP/1.1 200 OK") {
		t.Errorf("expected HTTP/1.1 200 OK, got: %s", got)
	}
	if !strings.Contains(got, "X-Test-Header: TestValue") {
		t.Errorf("expected X-Test-Header: TestValue, got: %s", got)
	}
	if !strings.Contains(got, "Content-Length: 13") {
		t.Errorf("expected Content-Length: 13, got: %s", got)
	}
	if !strings.HasSuffix(got, "Hello, world!") {
		t.Errorf("expected body 'Hello, world!', got: %s", got)
	}
}

func TestWriteResponse_NilBody(t *testing.T) {
	rw := httptest.NewRecorder()
	c := &Context{conn: &fakeConn{rw: rw}}
	c.writeResponseWithHeaders(NewDefaultLogger("test"), 200, nil)
	body := rw.Body.String()
	if !strings.Contains(body, "Content-Length: 0") {
		t.Errorf("expected Content-Length: 0, got: %q", body)
	}
	if strings.Contains(body, "Content-Type") {
		t.Errorf("expected no Content-Type for nil body")
	}
}

func TestWriteResponse_ContentTypePreserved(t *testing.T) {
	rw := httptest.NewRecorder()
	c := &Context{
		conn:      &fakeConn{rw: rw},
		resHeader: []KV{{K: []byte("Content-Type"), V: []byte("application/xml")}},
	}
	c.writeResponseWithHeaders(NewDefaultLogger("test"), 200, []byte("<x/>"))
	body := rw.Body.String()
	if strings.Count(body, "Content-Type") != 1 || !strings.Contains(body, "application/xml") {
		t.Errorf("unexpected Content-Type in response: %q", body)
	}
}

func TestWriteResponse_ContentLengthPreserved(t *testing.T) {
	rw := httptest.NewRecorder()
	c := &Context{
		conn:      &fakeConn{rw: rw},
		resHeader: []KV{{K: []byte("Content-Length"), V: []byte("999")}},
	}
	c.writeResponseWithHeaders(NewDefaultLogger("test"), 200, []byte("hi"))
	body := rw.Body.String()
	if strings.Count(body, "Content-Length") != 1 || !strings.Contains(body, "999") {
		t.Errorf("unexpected Content-Length in response: %q", body)
	}
}
func TestRedactSensitiveHeaders(t *testing.T) {
	block := "HTTP/1.1 200 OK\r\n" +
		"Set-Cookie: session=secret123\r\n" +
		"Authorization: Bearer token\r\n" +
		"Content-Type: text/plain\r\n"

	got := redactSensitiveHeaders(block)

	if strings.Contains(got, "secret123") || strings.Contains(got, "Bearer token") {
		t.Errorf("sensitive header value not redacted: %q", got)
	}
	if !strings.Contains(got, "Set-Cookie: [REDACTED]") {
		t.Errorf("expected redacted Set-Cookie, got: %q", got)
	}
	if !strings.Contains(got, "Authorization: [REDACTED]") {
		t.Errorf("expected redacted Authorization, got: %q", got)
	}
	if !strings.Contains(got, "Content-Type: text/plain") {
		t.Errorf("non-sensitive header must be preserved, got: %q", got)
	}
}

func TestWriteResponse_RedactsSensitiveHeaderInLog(t *testing.T) {
	var logged string
	logger := func(msgs ...string) { logged = strings.Join(msgs, "") }

	rw := httptest.NewRecorder()
	c := &Context{
		conn:      &fakeConn{rw: rw},
		resHeader: []KV{{K: []byte("Set-Cookie"), V: []byte("session=topsecret")}},
	}
	c.writeResponseWithHeaders(logger, 200, []byte("ok"))

	if strings.Contains(logged, "topsecret") {
		t.Errorf("Set-Cookie value leaked to log: %q", logged)
	}
	// The wire response must still carry the real cookie value.
	if !strings.Contains(rw.Body.String(), "session=topsecret") {
		t.Errorf("redaction must not alter the wire response: %q", rw.Body.String())
	}
}
