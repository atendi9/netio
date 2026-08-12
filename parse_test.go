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

// A line with no colon is not a header field. Skipping it — which is what the
// parser used to do — is the same disagreement as reshaping one: a proxy that
// rejects the line and a server that ignores it read different requests.
func TestParseHeaders_MalformedNoColon(t *testing.T) {
	req := "GET / HTTP/1.1\r\nBadHeaderWithoutColon\r\nHost: ok\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseBadReq {
		t.Errorf("parseRequest = %d, want parseBadReq", result)
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

// RFC 7230 §6.1 makes Connection a case-insensitive comma-separated list, and
// §6.3 defaults to close below HTTP/1.1. An exact byte comparison against
// "close" missed most real spellings and pinned the connection for the whole
// read deadline; treating HTTP/1.0 as persistent left those clients waiting.
func TestKeepAlive(t *testing.T) {
	connection := func(values ...string) []KV {
		kvs := make([]KV, 0, len(values))
		for _, v := range values {
			kvs = append(kvs, KV{K: []byte("connection"), V: []byte(v)})
		}
		return kvs
	}

	tests := []struct {
		name     string
		minor    int
		headers  []KV
		expected bool
	}{
		{name: "1.1 without Connection", minor: 1, headers: nil, expected: true},
		{name: "1.1 close", minor: 1, headers: connection("close"), expected: false},
		{name: "1.1 Close", minor: 1, headers: connection("Close"), expected: false},
		{name: "1.1 CLOSE", minor: 1, headers: connection("CLOSE"), expected: false},
		{name: "1.1 keep-alive", minor: 1, headers: connection("keep-alive"), expected: true},
		{name: "1.1 list ending in close", minor: 1, headers: connection("keep-alive, close"), expected: false},
		{name: "1.1 list starting with close", minor: 1, headers: connection("close, keep-alive"), expected: false},
		{name: "1.1 list with padding", minor: 1, headers: connection("Upgrade ,\tClose "), expected: false},
		{name: "1.1 close on a repeated field", minor: 1, headers: connection("keep-alive", "close"), expected: false},
		{name: "1.1 unrelated token", minor: 1, headers: connection("Upgrade"), expected: true},
		{name: "1.0 without Connection", minor: 0, headers: nil, expected: false},
		{name: "1.0 keep-alive", minor: 0, headers: connection("keep-alive"), expected: true},
		{name: "1.0 Keep-Alive", minor: 0, headers: connection("Keep-Alive"), expected: true},
		{name: "1.0 close", minor: 0, headers: connection("close"), expected: false},
		{name: "1.0 keep-alive and close", minor: 0, headers: connection("keep-alive, close"), expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Context{header: tt.headers, minorVersion: tt.minor}
			if got := keepAlive(c); got != tt.expected {
				t.Errorf("keepAlive() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// The version token was previously discarded, so anything in its place was
// served as a valid HTTP/1.1 request.
func TestParseRequestLine_Version(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		want  parseResult
		minor int
	}{
		{name: "HTTP/1.1", line: "GET / HTTP/1.1", want: parseOK, minor: 1},
		{name: "HTTP/1.0", line: "GET / HTTP/1.0", want: parseOK, minor: 0},
		{name: "HTTP/1.9 treated as 1.x", line: "GET / HTTP/1.9", want: parseOK, minor: 9},
		{name: "HTTP/2.0", line: "GET / HTTP/2.0", want: parseBadVersion},
		{name: "HTTP/0.9", line: "GET / HTTP/0.9", want: parseBadVersion},
		{name: "garbage token", line: "GET / JUNK", want: parseBadReq},
		{name: "missing minor", line: "GET / HTTP/1", want: parseBadReq},
		{name: "non-digit version", line: "GET / HTTP/x.y", want: parseBadReq},
		{name: "trailing junk after version", line: "GET / HTTP/1.1 extra", want: parseBadReq},
		{name: "empty version", line: "GET / ", want: parseBadReq},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bufio.NewReader(bytes.NewBufferString(tt.line + "\r\nHost: x\r\n\r\n"))
			c := &Context{}
			got := parseRequest(r, c)
			if got != tt.want {
				t.Fatalf("parseRequest = %d, want %d", got, tt.want)
			}
			if tt.want == parseOK && c.minorVersion != tt.minor {
				t.Errorf("minorVersion = %d, want %d", c.minorVersion, tt.minor)
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

// A request whose header block, body, or chunk framing is cut short must be
// rejected rather than dispatched: the truncation branches are what stop a peer
// that dies mid-request from reaching a handler with a half-read Context.
func TestParseRequest_Truncated(t *testing.T) {
	tests := []struct {
		name string
		req  string
		want parseResult
	}{
		{
			name: "header block with no terminating blank line",
			req:  "GET / HTTP/1.1\r\nHost: example.com",
			want: parseBadReq,
		},
		{
			name: "body shorter than Content-Length",
			req:  "POST / HTTP/1.1\r\ncontent-length: 10\r\n\r\nabc",
			want: parseBadReq,
		},
		{
			name: "chunked with no chunk-size line",
			req:  "POST / HTTP/1.1\r\ntransfer-encoding: chunked\r\n\r\n",
			want: parseBadReq,
		},
		{
			name: "chunk data shorter than its size",
			req:  "POST / HTTP/1.1\r\ntransfer-encoding: chunked\r\n\r\n5\r\nab",
			want: parseBadReq,
		},
		{
			name: "chunk data with no terminating CRLF",
			req:  "POST / HTTP/1.1\r\ntransfer-encoding: chunked\r\n\r\n5\r\nhello",
			want: parseBadReq,
		},
		{
			name: "chunk data followed by garbage instead of CRLF",
			req:  "POST / HTTP/1.1\r\ntransfer-encoding: chunked\r\n\r\n5\r\nhelloXX\r\n0\r\n\r\n",
			want: parseBadReq,
		},
		{
			name: "last-chunk with no terminating blank line",
			req:  "POST / HTTP/1.1\r\ntransfer-encoding: chunked\r\n\r\n0\r\n",
			want: parseBadReq,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bufio.NewReader(bytes.NewBufferString(tt.req))
			c := &Context{}
			if got := parseRequest(r, c); got != tt.want {
				t.Errorf("parseRequest = %d, want %d", got, tt.want)
			}
		})
	}
}

// Content-Length: 0 short-circuits before allocating, and must still leave the
// request parseable with an empty body.
func TestParseBody_ZeroContentLength(t *testing.T) {
	req := "POST /data HTTP/1.1\r\ncontent-length: 0\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Fatalf("parseRequest = %d, want parseOK", result)
	}
	if len(c.body) != 0 {
		t.Errorf("body = %q, want empty", c.body)
	}
}

// Trailers after the last chunk are consumed, not mistaken for the next
// pipelined request.
func TestParseChunked_WithTrailer(t *testing.T) {
	req := "POST / HTTP/1.1\r\ntransfer-encoding: chunked\r\n\r\n" +
		"5\r\nhello\r\n0\r\nX-Checksum: abc\r\n\r\n"
	r := bufio.NewReader(bytes.NewBufferString(req))
	c := &Context{}
	if result := parseRequest(r, c); result != parseOK {
		t.Fatalf("parseRequest = %d, want parseOK", result)
	}
	if string(c.body) != "hello" {
		t.Errorf("body = %q, want %q", c.body, "hello")
	}
	if r.Buffered() != 0 {
		t.Errorf("%d bytes left buffered, trailer was not consumed", r.Buffered())
	}
}

// RFC 7230 §3.2.4 requires rejecting a header the parser would otherwise have
// to reshape. Every case here is a framing disagreement a proxy in front of the
// server could be talked into: reshaping the field instead of rejecting it lets
// the two sides read different requests out of the same bytes.
func TestParseHeaders_MalformedFieldLine(t *testing.T) {
	tests := []struct {
		name string
		req  string
		want parseResult
	}{
		{
			name: "space between field-name and colon",
			req:  "POST / HTTP/1.1\r\nContent-Length : 5\r\n\r\nhello",
			want: parseBadReq,
		},
		{
			name: "tab between field-name and colon",
			req:  "POST / HTTP/1.1\r\nContent-Length\t: 5\r\n\r\nhello",
			want: parseBadReq,
		},
		{
			name: "obs-fold continuation line",
			req:  "POST / HTTP/1.1\r\nHost: x\r\n Content-Length: 5\r\n\r\nhello",
			want: parseBadReq,
		},
		{
			name: "leading whitespace on the first field line",
			req:  "POST / HTTP/1.1\r\n\tContent-Length: 5\r\n\r\nhello",
			want: parseBadReq,
		},
		{
			name: "empty field-name",
			req:  "POST / HTTP/1.1\r\n: 5\r\n\r\n",
			want: parseBadReq,
		},
		{
			name: "blank-looking line does not end the header block",
			req:  "POST / HTTP/1.1\r\nHost: x\r\n \r\nContent-Length: 5\r\n\r\nhello",
			want: parseBadReq,
		},
		{
			name: "well-formed headers still parse",
			req:  "POST / HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\n\r\nhello",
			want: parseOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bufio.NewReader(bytes.NewBufferString(tt.req))
			c := &Context{}
			if got := parseRequest(r, c); got != tt.want {
				t.Errorf("parseRequest = %d, want %d", got, tt.want)
			}
		})
	}
}

// RFC 7230 §3.3.3: repeated framing headers that disagree leave the message
// length undefined, so the request is unrecoverable rather than resolved by
// picking one value.
func TestParseBody_RepeatedFramingHeaders(t *testing.T) {
	tests := []struct {
		name string
		req  string
		want parseResult
	}{
		{
			name: "conflicting Content-Length",
			req:  "POST / HTTP/1.1\r\nContent-Length: 5\r\nContent-Length: 11\r\n\r\nhelloworld!",
			want: parseBadReq,
		},
		{
			name: "identical Content-Length repeated",
			req:  "POST / HTTP/1.1\r\nContent-Length: 5\r\nContent-Length: 5\r\n\r\nhello",
			want: parseOK,
		},
		{
			name: "repeated Transfer-Encoding",
			req:  "POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\nTransfer-Encoding: identity\r\n\r\n0\r\n\r\n",
			want: parseBadReq,
		},
		{
			name: "Content-Length and Transfer-Encoding together",
			req:  "POST / HTTP/1.1\r\nContent-Length: 5\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n",
			want: parseBadReq,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bufio.NewReader(bytes.NewBufferString(tt.req))
			c := &Context{}
			if got := parseRequest(r, c); got != tt.want {
				t.Errorf("parseRequest = %d, want %d", got, tt.want)
			}
		})
	}
}

// Content-Length is 1*DIGIT (RFC 7230 §3.3.2) and chunk-size is 1*HEXDIG
// (§4.1). strconv accepts a leading sign on both, which this server must not:
// a value it reads as a length while a strict proxy rejects the field outright
// is a body-length disagreement.
func TestParseBody_FramingGrammar(t *testing.T) {
	contentLengths := []struct {
		value string
		want  parseResult
	}{
		{"5", parseOK},
		{"  5  ", parseOK}, // OWS around a field-value is allowed and trimmed
		{"+5", parseBadReq},
		{"-0", parseBadReq},
		{"-5", parseBadReq},
		{"0x5", parseBadReq},
		{"5abc", parseBadReq},
		{"", parseBadReq},
		{"99999999999999999999", parseBadReq}, // overflows int
	}

	for _, tt := range contentLengths {
		t.Run("Content-Length "+tt.value, func(t *testing.T) {
			req := "POST / HTTP/1.1\r\nContent-Length: " + tt.value + "\r\n\r\nhello"
			r := bufio.NewReader(bytes.NewBufferString(req))
			c := &Context{}
			if got := parseRequest(r, c); got != tt.want {
				t.Errorf("parseRequest = %d, want %d", got, tt.want)
			}
		})
	}

	chunkSizes := []struct {
		value string
		want  parseResult
	}{
		{"5", parseOK},
		{"5;ext=1", parseOK}, // chunk extensions stay allowed
		{"+5", parseBadReq},
		{"-5", parseBadReq},
		{"0x5", parseBadReq},
		{"", parseBadReq},
		{"FFFFFFFFFFFFFFFFF", parseBadReq}, // hex digits that overflow int64
	}

	for _, tt := range chunkSizes {
		t.Run("chunk-size "+tt.value, func(t *testing.T) {
			req := "POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n" +
				tt.value + "\r\nhello\r\n0\r\n\r\n"
			r := bufio.NewReader(bytes.NewBufferString(req))
			c := &Context{}
			if got := parseRequest(r, c); got != tt.want {
				t.Errorf("parseRequest = %d, want %d", got, tt.want)
			}
		})
	}

	t.Run("uppercase hex chunk-size", func(t *testing.T) {
		req := "POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\nA\r\n0123456789\r\n0\r\n\r\n"
		r := bufio.NewReader(bytes.NewBufferString(req))
		c := &Context{}
		if got := parseRequest(r, c); got != parseOK {
			t.Fatalf("parseRequest = %d, want parseOK", got)
		}
		if string(c.body) != "0123456789" {
			t.Errorf("body = %q, want %q", c.body, "0123456789")
		}
	})
}

// RFC 7230 §5.3.2 requires accepting the absolute-form request-target, which is
// what a proxy forwards. Routing on the raw target 404s every proxied request.
func TestParseRequestLine_AbsoluteForm(t *testing.T) {
	tests := []struct {
		name  string
		uri   string
		path  string
		query []KV
	}{
		{name: "origin-form", uri: "/users/1", path: "/users/1"},
		{name: "absolute-form", uri: "http://example.com/users/1", path: "/users/1"},
		{name: "absolute-form https", uri: "https://example.com/users/1", path: "/users/1"},
		{name: "absolute-form with port", uri: "http://example.com:8080/users/1", path: "/users/1"},
		{
			name:  "absolute-form with query",
			uri:   "http://example.com/search?q=go&n=2",
			path:  "/search",
			query: []KV{{[]byte("q"), []byte("go")}, {[]byte("n"), []byte("2")}},
		},
		{name: "absolute-form with no path", uri: "http://example.com", path: "/"},
		// Asterisk-form is neither origin- nor absolute-form; it passes through
		// untouched and falls to the OPTIONS handling in serve().
		{name: "asterisk-form passes through", uri: "*", path: "*"},
		{
			name:  "origin-form is not mistaken for an authority",
			uri:   "/redirect?to=http://example.com/x",
			path:  "/redirect",
			query: []KV{{[]byte("to"), []byte("http://example.com/x")}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := bufio.NewReader(bytes.NewBufferString("GET " + tt.uri + " HTTP/1.1\r\nHost: example.com\r\n\r\n"))
			c := &Context{}
			if res := parseRequest(r, c); res != parseOK {
				t.Fatalf("parseRequest = %d, want parseOK", res)
			}
			if string(c.path) != tt.path {
				t.Errorf("path = %q, want %q", c.path, tt.path)
			}
			if len(c.query) != len(tt.query) {
				t.Fatalf("query = %v, want %v", c.query, tt.query)
			}
			for i, kv := range tt.query {
				if !bytes.Equal(c.query[i].K, kv.K) || !bytes.Equal(c.query[i].V, kv.V) {
					t.Errorf("query[%d] = %s=%s, want %s=%s", i, c.query[i].K, c.query[i].V, kv.K, kv.V)
				}
			}
		})
	}
}

// RFC 7231 §5.1.1: only 100-continue is defined, and HTTP/1.0 has no interim
// responses so the field is ignored there.
func TestExpectation(t *testing.T) {
	expect := func(values ...string) []KV {
		kvs := make([]KV, 0, len(values))
		for _, v := range values {
			kvs = append(kvs, KV{K: []byte("expect"), V: []byte(v)})
		}
		return kvs
	}

	tests := []struct {
		name    string
		minor   int
		headers []KV
		want    parseResult
	}{
		{name: "no Expect", minor: 1, headers: nil, want: parseOK},
		{name: "100-continue", minor: 1, headers: expect("100-continue"), want: parseContinue},
		{name: "case-insensitive", minor: 1, headers: expect("100-Continue"), want: parseContinue},
		{name: "padded", minor: 1, headers: expect("  100-continue "), want: parseContinue},
		{name: "unknown expectation", minor: 1, headers: expect("the-impossible"), want: parseBadExpect},
		{name: "one known one unknown", minor: 1, headers: expect("100-continue", "nope"), want: parseBadExpect},
		{name: "HTTP/1.0 ignores it", minor: 0, headers: expect("100-continue"), want: parseOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Context{header: tt.headers, minorVersion: tt.minor}
			if got := expectation(c); got != tt.want {
				t.Errorf("expectation() = %d, want %d", got, tt.want)
			}
		})
	}
}
