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

	t.Run("request with query string", func(t *testing.T) {
		req := "GET /dashboard/test@test.com/all?startDate=26/03/2026%2008:00&endDate=26/03/2026%2018:00&duration=seconds HTTP/1.1\r\n\r\n"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		if !ok {
			t.Fatalf("parseRequest failed")
		}

		if !bytes.Equal(c.path, []byte("/dashboard/test@test.com/all")) {
			t.Errorf("path: got %q, want /dashboard/test@test.com/all", c.path)
		}

		if len(c.query) != 3 {
			t.Fatalf("expected 3 query params, got %d", len(c.query))
		}

		expectedQuery := []KV{
			{[]byte("startDate"), []byte("26/03/2026%2008:00")},
			{[]byte("endDate"), []byte("26/03/2026%2018:00")},
			{[]byte("duration"), []byte("seconds")},
		}
		for i, kv := range c.query {
			if !bytes.Equal(kv.K, expectedQuery[i].K) || !bytes.Equal(kv.V, expectedQuery[i].V) {
				t.Errorf("query[%d]: got %s=%s, want %s=%s", i, kv.K, kv.V, expectedQuery[i].K, expectedQuery[i].V)
			}
		}
	})

	t.Run("request without query string", func(t *testing.T) {
		req := "GET /hello HTTP/1.1\r\n\r\n"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		if !ok {
			t.Fatalf("parseRequest failed")
		}
		if !bytes.Equal(c.path, []byte("/hello")) {
			t.Errorf("path: got %q, want /hello", c.path)
		}
		if len(c.query) != 0 {
			t.Errorf("expected no query params, got %d", len(c.query))
		}
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