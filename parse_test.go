package netio

import (
	"bufio"
	"bytes"
	"testing"
)

func TestParse(t *testing.T) {
	t.Run("simple GET request", func(t *testing.T) {
		req := "GET /hello HTTP/1.1\r\n\r\n"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		if !ok {
			t.Fatalf("parseRequest failed")
		}

		if !bytes.Equal(c.method, []byte("GET")) {
			t.Errorf("method: got %s, want GET", c.method)
		}
		if !bytes.Equal(c.path, []byte("/hello")) {
			t.Errorf("path: got %s, want /hello", c.path)
		}
		if len(c.header) != 0 {
			t.Errorf("expected no headers, got %v", c.header)
		}
		if len(c.body) != 0 {
			t.Errorf("expected empty body, got %q", c.body)
		}
	})

	t.Run("request with headers", func(t *testing.T) {
		req := "POST /submit HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test\r\n\r\n"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		if !ok {
			t.Fatalf("parseRequest failed")
		}

		if !bytes.Equal(c.method, []byte("POST")) {
			t.Errorf("method: got %s, want POST", c.method)
		}
		if !bytes.Equal(c.path, []byte("/submit")) {
			t.Errorf("path: got %s, want /submit", c.path)
		}
		if len(c.header) != 2 {
			t.Fatalf("expected 2 headers, got %d", len(c.header))
		}
		expected := []KV{
			{[]byte("Host"), []byte("example.com")},
			{[]byte("User-Agent"), []byte("test")},
		}
		for i, kv := range c.header {
			if !bytes.Equal(kv.K, expected[i].K) || !bytes.Equal(kv.V, expected[i].V) {
				t.Errorf("header[%d]: got %s: %s, want %s: %s", i, kv.K, kv.V, expected[i].K, expected[i].V)
			}
		}
	})

	t.Run("request with body", func(t *testing.T) {
		req := "POST /data HTTP/1.1\r\nContent-Length: 5\r\n\r\nhello"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		if !ok {
			t.Fatalf("parseRequest failed")
		}

		if !bytes.Equal(c.body, []byte("hello")) {
			t.Errorf("body: got %q, want hello", c.body)
		}
	})

	t.Run("more than 16 headers", func(t *testing.T) {
		var buf bytes.Buffer
		buf.WriteString("GET /many-headers HTTP/1.1\r\n")
		for i := 0; i < 20; i++ {
			buf.WriteString("X-Header-" + string(rune('A'+i)) + ": value-" + string(rune('A'+i)) + "\r\n")
		}
		buf.WriteString("\r\n")

		r := bufio.NewReader(&buf)
		c := ctxPool.Get().(*Context)
		c.reset()

		ok := parseRequest(r, c)
		if !ok {
			t.Fatalf("parseRequest failed")
		}

		if len(c.header) != 20 {
			t.Fatalf("expected 20 headers, got %d", len(c.header))
		}

		for i := 0; i < 20; i++ {
			key := "X-Header-" + string(rune('A'+i))
			val := c.Header(key)
			expected := "value-" + string(rune('A'+i))
			if val != expected {
				t.Errorf("header %s: got %q, want %q", key, val, expected)
			}
		}

		ctxPool.Put(c)
	})

	t.Run("headers are independent copies", func(t *testing.T) {
		req := "GET /test HTTP/1.1\r\nX-Auth: secret-token-123\r\nX-Id: abc-456\r\n\r\n"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		if !ok {
			t.Fatalf("parseRequest failed")
		}

		auth := c.Header("X-Auth")
		id := c.Header("X-Id")

		if auth != "secret-token-123" {
			t.Errorf("X-Auth: got %q, want secret-token-123", auth)
		}
		if id != "abc-456" {
			t.Errorf("X-Id: got %q, want abc-456", id)
		}

		// Verify header bytes are independent copies (not sharing backing array with line)
		for _, kv := range c.header {
			if cap(kv.K) > len(kv.K) {
				// If cap > len, it might share backing array with the original line
				// Our fix ensures exact-size copies
			}
			if cap(kv.K) != len(kv.K) {
				t.Errorf("header key %q should be an exact copy (cap=%d, len=%d)", kv.K, cap(kv.K), len(kv.K))
			}
			if cap(kv.V) != len(kv.V) {
				t.Errorf("header value %q should be an exact copy (cap=%d, len=%d)", kv.V, cap(kv.V), len(kv.V))
			}
		}
	})

	t.Run("pool reuse preserves headers", func(t *testing.T) {
		// Simulate pool reuse: first request with many headers, then second with few
		c := ctxPool.Get().(*Context)
		c.reset()

		var buf1 bytes.Buffer
		buf1.WriteString("GET /first HTTP/1.1\r\n")
		for i := 0; i < 20; i++ {
			buf1.WriteString("X-First-" + string(rune('A'+i)) + ": first-" + string(rune('A'+i)) + "\r\n")
		}
		buf1.WriteString("\r\n")

		r1 := bufio.NewReader(&buf1)
		parseRequest(r1, c)

		if len(c.header) != 20 {
			t.Fatalf("first request: expected 20 headers, got %d", len(c.header))
		}

		ctxPool.Put(c)

		// Second request reuses pool context
		c2 := ctxPool.Get().(*Context)
		c2.reset()

		req2 := "GET /second HTTP/1.1\r\nX-Only: one\r\n\r\n"
		r2 := bufio.NewReader(bytes.NewBufferString(req2))
		parseRequest(r2, c2)

		if len(c2.header) != 1 {
			t.Fatalf("second request: expected 1 header, got %d", len(c2.header))
		}

		if c2.Header("X-Only") != "one" {
			t.Errorf("second request header: got %q, want one", c2.Header("X-Only"))
		}

		// Ensure no leftover from first request
		if c2.Header("X-First-A") != "" {
			t.Errorf("second request has leftover header from first request")
		}

		ctxPool.Put(c2)
	})

	t.Run("chunked body request", func(t *testing.T) {
		req := "POST /chunked HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n" +
			"5\r\nhello\r\n" +
			"6\r\n world\r\n" +
			"0\r\n\r\n"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		if !ok {
			t.Fatalf("parseRequest failed")
		}

		if !bytes.Equal(c.body, []byte("hello world")) {
			t.Errorf("body: got %q, want 'hello world'", c.body)
		}
	})
}
