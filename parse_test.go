package netio

import (
	"bufio"
	"bytes"
	"testing"
)

func TestParseRequest_EmptyReader(t *testing.T) {
	r := bufio.NewReader(bytes.NewReader(nil))
	c := &Context{
		method: make([]byte, 0),
		path:   make([]byte, 0),
		header: make([]KV, 0),
		query:  make([]KV, 0),
	}
	if result := parseRequest(r, c); result == parseOK {
		t.Error("expected non-OK result for empty reader")
	}
}

func TestParseRequest_SimpleGET(t *testing.T) {
	r := bufio.NewReader(bytes.NewBufferString("GET /hello HTTP/1.1\r\n\r\n"))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Fatalf("parseRequest failed with %d", result)
	}
	if !bytes.Equal(c.method, []byte("GET")) {
		t.Errorf("expected GET, got %s", c.method)
	}
	if !bytes.Equal(c.path, []byte("/hello")) {
		t.Errorf("expected /hello, got %s", c.path)
	}
	if len(c.header) != 0 {
		t.Errorf("expected no headers, got %v", c.header)
	}
}

func TestParseRequest_WithHeaders(t *testing.T) {
	req := "POST /submit HTTP/1.1\r\nHost: example.com\r\nUser-Agent: test\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Fatalf("parseRequest failed with %d", result)
	}
	if len(c.header) != 2 {
		t.Fatalf("expected 2 headers, got %d", len(c.header))
	}
	expected := []KV{
		{[]byte("host"), []byte("example.com")},
		{[]byte("user-agent"), []byte("test")},
	}
	for i, kv := range c.header {
		if !bytes.Equal(kv.K, expected[i].K) || !bytes.Equal(kv.V, expected[i].V) {
			t.Errorf("header[%d]: got %s=%s, want %s=%s", i, kv.K, kv.V, expected[i].K, expected[i].V)
		}
	}
}

func TestParseRequest_WithBody(t *testing.T) {
	req := "POST /data HTTP/1.1\r\nContent-Length: 5\r\n\r\nhello"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Fatalf("parseRequest failed with %d", result)
	}
	if !bytes.Equal(c.body, []byte("hello")) {
		t.Errorf("expected hello, got %q", c.body)
	}
}

func TestParseRequest_WithQueryString(t *testing.T) {
	req := "GET /path?a=1&b=2 HTTP/1.1\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Fatalf("parseRequest failed with %d", result)
	}
	if !bytes.Equal(c.path, []byte("/path")) {
		t.Errorf("expected /path, got %s", c.path)
	}
	if len(c.query) != 2 {
		t.Fatalf("expected 2 query params, got %d", len(c.query))
	}
}

func TestParseRequest_ChunkedBody(t *testing.T) {
	req := "POST /chunked HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n" +
		"5\r\nhello\r\n6\r\n world\r\n0\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Fatalf("parseRequest failed with %d", result)
	}
	if !bytes.Equal(c.body, []byte("hello world")) {
		t.Errorf("expected 'hello world', got %q", c.body)
	}
}

func TestParseQueryString_KeyValue(t *testing.T) {
	c := &Context{query: make([]KV, 0)}
	parseQueryString([]byte("a=1&b=2"), c)
	if len(c.query) != 2 {
		t.Fatalf("expected 2 params, got %d", len(c.query))
	}
}

func TestParseQueryString_KeyOnly(t *testing.T) {
	c := &Context{query: make([]KV, 0)}
	parseQueryString([]byte("flag"), c)
	if len(c.query) != 1 || string(c.query[0].K) != "flag" || c.query[0].V != nil {
		t.Errorf("unexpected result: %v", c.query)
	}
}

func TestParseQueryString_EmptyPairs(t *testing.T) {
	c := &Context{query: make([]KV, 0)}
	parseQueryString([]byte("&&a=1&&"), c)
	if len(c.query) != 1 || string(c.query[0].K) != "a" || string(c.query[0].V) != "1" {
		t.Errorf("unexpected result: %v", c.query)
	}
}

func TestParseRequestLine_MalformedNoSpace(t *testing.T) {
	r := bufio.NewReader(bytes.NewBufferString("GETNOHTTP\r\n\r\n"))
	c := &Context{}
	if result := parseRequest(r, c); result != parseBadReq {
		t.Errorf("expected parseBadReq, got %d", result)
	}
}

func TestParseRequestLine_MalformedOneSpace(t *testing.T) {
	r := bufio.NewReader(bytes.NewBufferString("GET /pathonly\r\n\r\n"))
	c := &Context{}
	if result := parseRequest(r, c); result != parseBadReq {
		t.Errorf("expected parseBadReq, got %d", result)
	}
}

func TestParseHeaders_MalformedNoColon(t *testing.T) {
	req := "GET / HTTP/1.1\r\nBadHeaderWithoutColon\r\nHost: ok\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Fatalf("parseRequest failed with %d", result)
	}
	// The malformed header is skipped, only Host is parsed
	if len(c.header) != 1 || string(c.header[0].K) != "host" {
		t.Errorf("expected only 'host' header, got %v", c.header)
	}
}

func TestParseBody_InvalidContentLength(t *testing.T) {
	req := "GET / HTTP/1.1\r\ncontent-length: abc\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseBadReq {
		t.Errorf("expected parseBadReq for invalid content-length, got %d", result)
	}
}

func TestParseBody_ExceedsMaxBodySize(t *testing.T) {
	req := "POST /data HTTP/1.1\r\ncontent-length: 999\r\n\r\n" + string(make([]byte, 999))
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{maxBodySize: 10}
	if result := parseRequest(r, c); result != parseTooLarge {
		t.Errorf("expected parseTooLarge, got %d", result)
	}
}

func TestParseBody_ChunkedExceedsMaxBodySize(t *testing.T) {
	req := "POST /chunked HTTP/1.1\r\ntransfer-encoding: chunked\r\n\r\n" +
		"14\r\n" + string(make([]byte, 20)) + "\r\n0\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{maxBodySize: 10}
	if result := parseRequest(r, c); result != parseTooLarge {
		t.Errorf("expected parseTooLarge, got %d", result)
	}
}

func TestParseRequest_OversizedRequestLine(t *testing.T) {
	// A request line longer than maxRequestLineSize must be rejected, not
	// accumulated unboundedly in memory.
	hugeURI := bytes.Repeat([]byte("a"), maxRequestLineSize+1)
	req := append([]byte("GET /"), hugeURI...)
	req = append(req, []byte(" HTTP/1.1\r\n\r\n")...)
	r := bufio.NewReader(bytes.NewReader(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseBadReq {
		t.Errorf("expected parseBadReq for oversized request line, got %d", result)
	}
}

func TestParseHeaders_OversizedHeaderLine(t *testing.T) {
	hugeValue := bytes.Repeat([]byte("a"), maxHeaderLineSize+1)
	req := []byte("GET / HTTP/1.1\r\nX-Big: ")
	req = append(req, hugeValue...)
	req = append(req, []byte("\r\n\r\n")...)
	r := bufio.NewReader(bytes.NewReader(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseBadReq {
		t.Errorf("expected parseBadReq for oversized header line, got %d", result)
	}
}

func TestParseHeaders_TooManyHeaders(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("GET / HTTP/1.1\r\n")
	for i := 0; i <= maxHeaderCount; i++ {
		buf.WriteString("X-H: v\r\n")
	}
	buf.WriteString("\r\n")
	r := bufio.NewReader(&buf)
	c := &Context{}
	if result := parseRequest(r, c); result != parseBadReq {
		t.Errorf("expected parseBadReq for too many headers, got %d", result)
	}
}

func TestParseHeaders_TruncatedBlock(t *testing.T) {
	// Connection dies mid-headers: no terminating blank line. Must be rejected,
	// not dispatched as a complete request.
	req := "GET / HTTP/1.1\r\nHost: example.com\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseBadReq {
		t.Errorf("expected parseBadReq for truncated header block, got %d", result)
	}
}

func TestParseBody_RejectsContentLengthAndTransferEncoding(t *testing.T) {
	// RFC 7230 §3.3.3: a request with both headers is ambiguous (smuggling).
	req := "POST / HTTP/1.1\r\nContent-Length: 5\r\nTransfer-Encoding: chunked\r\n\r\nhello"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseBadReq {
		t.Errorf("expected parseBadReq for CL+TE request, got %d", result)
	}
}

func TestParseBody_ChunkedCaseInsensitive(t *testing.T) {
	req := "POST /c HTTP/1.1\r\nTransfer-Encoding: Chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Fatalf("expected parseOK for 'Chunked', got %d", result)
	}
	if !bytes.Equal(c.body, []byte("hello")) {
		t.Errorf("expected 'hello', got %q", c.body)
	}
}

func TestParseBody_ChunkedAsFinalCoding(t *testing.T) {
	req := "POST /c HTTP/1.1\r\nTransfer-Encoding: gzip, chunked\r\n\r\n5\r\nhello\r\n0\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Fatalf("expected parseOK for 'gzip, chunked', got %d", result)
	}
	if !bytes.Equal(c.body, []byte("hello")) {
		t.Errorf("expected 'hello', got %q", c.body)
	}
}

func TestParseBody_UnknownTransferEncoding(t *testing.T) {
	// An unknown transfer coding must be rejected, not silently treated as
	// "no body" (which would leave the coding's framing bytes unconsumed).
	req := "POST /c HTTP/1.1\r\nTransfer-Encoding: bogus\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseBadReq {
		t.Errorf("expected parseBadReq for unknown transfer-encoding, got %d", result)
	}
}

func TestParseBody_IdentityTransferEncoding(t *testing.T) {
	req := "POST /c HTTP/1.1\r\nTransfer-Encoding: identity\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Errorf("expected parseOK for identity transfer-encoding, got %d", result)
	}
}

func TestKeepAlive(t *testing.T) {
	tests := []struct {
		name     string
		headers  []KV
		expected bool
	}{
		{
			name:     "Connection close",
			headers:  []KV{{K: []byte("connection"), V: []byte("close")}},
			expected: false,
		},
		{
			name:     "Connection keep-alive",
			headers:  []KV{{K: []byte("connection"), V: []byte("keep-alive")}},
			expected: true,
		},
		{
			name:     "Connection missing",
			headers:  []KV{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Context{header: tt.headers}
			got := keepAlive(c)
			if got != tt.expected {
				t.Errorf("keepAlive() = %v; want %v", got, tt.expected)
			}
		})
	}
}

func TestParseChunked_NegativeSizeRejected(t *testing.T) {
	// A negative chunk size must be rejected, not parsed into a make() length.
	req := "POST /c HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n-1\r\nx\r\n0\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseBadReq {
		t.Fatalf("expected parseBadReq for negative chunk size, got %d", result)
	}
}

func TestParseChunked_ChunkExtensionIgnored(t *testing.T) {
	// A chunk-size line may carry a ";ext" extension that must be stripped.
	req := "POST /c HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n" +
		"5;name=value\r\nhello\r\n0\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Fatalf("expected parseOK with chunk extension, got %d", result)
	}
	if !bytes.Equal(c.body, []byte("hello")) {
		t.Errorf("expected 'hello', got %q", c.body)
	}
}

func TestParseChunked_TrailerConsumed(t *testing.T) {
	// Trailer headers after the last-chunk marker must be consumed cleanly.
	req := "POST /c HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n" +
		"5\r\nhello\r\n0\r\nX-Trailer: v\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Fatalf("expected parseOK with trailer, got %d", result)
	}
	if !bytes.Equal(c.body, []byte("hello")) {
		t.Errorf("expected 'hello', got %q", c.body)
	}
}

func TestParseChunked_BadChunkTerminator(t *testing.T) {
	// A chunk whose data is not followed by CRLF is corrupt framing.
	req := "POST /c HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n" +
		"5\r\nhelloXX0\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseBadReq {
		t.Fatalf("expected parseBadReq for bad chunk terminator, got %d", result)
	}
}
