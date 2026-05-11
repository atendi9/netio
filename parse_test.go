package netio

import (
	"bufio"
	"bytes"
	"testing"

	"github.com/atendi9/capivara/assert"
)

func TestParse(t *testing.T) {
	t.Run("simple GET request", func(t *testing.T) {
		req := "GET /hello HTTP/1.1\r\n\r\n"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		assert.True(t, ok)
		assert.True(t, bytes.Equal(c.method, []byte("GET")))
		assert.True(t, bytes.Equal(c.path, []byte("/hello")))
		assert.LengthSlice(t, 0, c.header)
		assert.LengthSlice(t, 0, c.body)
	})

	t.Run("request with headers", func(t *testing.T) {
		req := "POST /submit HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test\r\n\r\n"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		assert.True(t, ok)
		assert.True(t, bytes.Equal(c.method, []byte("POST")))
		assert.True(t, bytes.Equal(c.path, []byte("/submit")))
		assert.LengthSlice(t, 2, c.header)
		expected := []KV{
			{[]byte("Host"), []byte("example.com")},
			{[]byte("User-Agent"), []byte("test")},
		}
		for i, kv := range c.header {
			ok = bytes.Equal(kv.K, expected[i].K) || !bytes.Equal(kv.V, expected[i].V)
			assert.True(t, ok)
		}
	})

	t.Run("request with body", func(t *testing.T) {
		req := "POST /data HTTP/1.1\r\nContent-Length: 5\r\n\r\nhello"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		assert.True(t, ok)
		assert.True(t, bytes.Equal(c.body, []byte("hello")))
	})

	t.Run("request with query string", func(t *testing.T) {
		req := "GET /dashboard/test@test.com/all?startDate=26/03/2026%2008:00&endDate=26/03/2026%2018:00&duration=seconds HTTP/1.1\r\n\r\n"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		assert.True(t, ok)
		assert.True(t, bytes.Equal(c.path, []byte("/dashboard/test@test.com/all")))
		assert.LengthSlice(t, 3, c.query)

		expectedQuery := []KV{
			{[]byte("startDate"), []byte("26/03/2026%2008:00")},
			{[]byte("endDate"), []byte("26/03/2026%2018:00")},
			{[]byte("duration"), []byte("seconds")},
		}
		for i, kv := range c.query {
			ok = bytes.Equal(kv.K, expectedQuery[i].K) || !bytes.Equal(kv.V, expectedQuery[i].V)
			assert.True(t, ok)
		}
	})

	t.Run("request without query string", func(t *testing.T) {
		req := "GET /hello HTTP/1.1\r\n\r\n"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		assert.True(t, ok)
		ok = bytes.Equal(c.path, []byte("/hello"))
		assert.True(t, ok)
		assert.LengthSlice(t, 0, c.query)
	})

	t.Run("chunked body request", func(t *testing.T) {
		req := "POST /chunked HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n" +
			"5\r\nhello\r\n" +
			"6\r\n world\r\n" +
			"0\r\n\r\n"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		ok := parseRequest(r, c)
		assert.True(t, ok)
		ok = bytes.Equal(c.body, []byte("hello world"))
		assert.True(t, ok)
	})
}
