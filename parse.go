package netio

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"net/url"
	"strconv"
)

// parseResult indicates the outcome of parsing a request.
type parseResult int

const (
	parseOK       parseResult = 0
	parseEOF      parseResult = -1
	parseTooLarge parseResult = 413
	parseBadReq   parseResult = 400
	// parseBadVersion marks a request whose HTTP major version this server does
	// not implement. RFC 7230 §2.6 asks for 505 rather than a blanket 400.
	parseBadVersion parseResult = 505
	// parseBadExpect marks an Expect the server cannot meet (RFC 7231 §5.1.1).
	parseBadExpect parseResult = 417
)

// Parser limits, mirroring the protections net/http provides. A hand-rolled
// parser without these lets a malicious peer stream an unbounded request line
// or header block until the process runs out of memory.
const (
	// maxRequestLineSize caps the HTTP request line (method + URI + version).
	maxRequestLineSize = 8 * 1024
	// maxHeaderLineSize caps a single header line (name + value).
	maxHeaderLineSize = 8 * 1024
	// maxHeaderCount caps the number of headers in a request.
	maxHeaderCount = 100
)

func parseRequest(r *bufio.Reader, c *Context) parseResult {
	if res := parseRequestHead(r, c); res != parseOK {
		return res
	}

	return parseBody(r, c)
}

// parseRequestHead reads the request line and header block, stopping short of
// the body so the caller can answer an Expect: 100-continue before the client
// starts uploading.
func parseRequestHead(r *bufio.Reader, c *Context) parseResult {
	line, err := readLimitedLine(r, maxRequestLineSize)
	if err != nil {
		if err == errLineTooLong {
			return parseBadReq
		}
		return parseEOF
	}

	if res := parseRequestLine(line, c); res != parseOK {
		return res
	}

	return parseHeaders(r, c)
}

// expectation classifies the request's Expect field. RFC 7231 §5.1.1 defines
// only "100-continue"; anything else the server does not understand is a 417
// rather than a header to read past. HTTP/1.0 has no interim responses, so the
// field is ignored there.
func expectation(c *Context) parseResult {
	values := headerValues(c, []byte("expect"))
	if len(values) == 0 {
		return parseOK
	}

	for _, v := range values {
		if !bytes.EqualFold(bytes.TrimSpace(v), []byte("100-continue")) {
			return parseBadExpect
		}
	}
	if c.minorVersion == 0 {
		return parseOK
	}

	return parseContinue
}

// parseContinue reports that the client is waiting for a 100 Continue before it
// sends the body.
const parseContinue parseResult = 100

var errLineTooLong = errors.New("netio: line exceeds size limit")

// readLimitedLine reads a single CRLF/LF-terminated line, returning
// errLineTooLong if it exceeds limit before a newline is seen. This prevents
// bufio.Reader from accumulating an unbounded line into memory.
func readLimitedLine(r *bufio.Reader, limit int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > limit {
			return nil, errLineTooLong
		}
		if err == nil {
			return line, nil
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		return line, err
	}
}

func parseRequestLine(line []byte, c *Context) parseResult {
	i := bytes.IndexByte(line, ' ')
	if i == -1 {
		return parseBadReq
	}
	c.method = line[:i]

	rest := line[i+1:]
	j := bytes.IndexByte(rest, ' ')
	if j == -1 {
		return parseBadReq
	}
	uri := rest[:j]

	// The version token used to be discarded, which served any garbage in its
	// place as a valid request and left every client looking like HTTP/1.1.
	major, minor, ok := parseHTTPVersion(bytes.TrimRight(rest[j+1:], "\r\n"))
	if !ok {
		return parseBadReq
	}
	if major != 1 {
		return parseBadVersion
	}
	c.minorVersion = minor

	uri = requestTargetPath(uri)

	if q := bytes.IndexByte(uri, '?'); q != -1 {
		c.path = uri[:q]
		parseQueryString(uri[q+1:], c)
	} else {
		c.path = uri
	}
	return parseOK
}

// requestTargetPath reduces a request-target to the path the router matches on.
// RFC 7230 §5.3.2 requires accepting the absolute-form ("GET http://host/p"),
// which every proxy forwards verbatim: routing on the raw target instead 404s
// any request that reached the server through one.
func requestTargetPath(uri []byte) []byte {
	if len(uri) == 0 || uri[0] == '/' {
		return uri
	}

	i := bytes.Index(uri, []byte("://"))
	if i == -1 {
		return uri
	}

	authority := uri[i+len("://"):]
	if j := bytes.IndexByte(authority, '/'); j != -1 {
		return authority[j:]
	}

	// "http://example.com" with no path addresses the origin's root.
	return []byte("/")
}

// parseHTTPVersion parses the "HTTP/<major>.<minor>" token of a request line.
// RFC 7230 §2.6 fixes both numbers at exactly one DIGIT.
func parseHTTPVersion(v []byte) (major, minor int, ok bool) {
	const prefix = "HTTP/"
	if len(v) != len(prefix)+3 || !bytes.HasPrefix(v, []byte(prefix)) {
		return 0, 0, false
	}
	maj, dot, min := v[len(prefix)], v[len(prefix)+1], v[len(prefix)+2]
	if !isDigit(maj) || dot != '.' || !isDigit(min) {
		return 0, 0, false
	}
	return int(maj - '0'), int(min - '0'), true
}

// parseHeaders reads the header block. It distinguishes a clean end-of-headers
// (blank line) from a read error or an oversized/over-count header block: a
// connection that dies mid-headers must not be dispatched as a valid request.
func parseHeaders(r *bufio.Reader, c *Context) parseResult {
	for {
		line, err := readLimitedLine(r, maxHeaderLineSize)
		if err == errLineTooLong {
			return parseBadReq
		}

		// Only the line terminator is stripped: a line of blanks is a malformed
		// header, not the end of the block. Treating it as a terminator would
		// end the request here while a stricter proxy read on, which is exactly
		// the disagreement request smuggling exploits.
		field := bytes.TrimRight(line, "\r\n")

		if len(field) == 0 {
			if err != nil {
				// Truncated header block: no terminating blank line.
				return parseBadReq
			}
			return parseOK
		}

		// A non-blank line followed by a read error means the header block
		// was cut off mid-line — reject rather than dispatch a partial request.
		if err != nil {
			return parseBadReq
		}

		if len(c.header) >= maxHeaderCount {
			return parseBadReq
		}

		// RFC 7230 §3.2.4: a server MUST reject a request whose header line
		// begins with whitespace (obs-fold) or whose field-name is separated
		// from the colon by whitespace. Trimming the name into shape instead is
		// the classic smuggling primitive: a proxy that rejects "Content-Length
		// : 5" and a server that honours it disagree on where the body ends.
		if isOWS(field[0]) {
			return parseBadReq
		}

		i := bytes.IndexByte(field, ':')
		if i == -1 {
			// A line that is not a field at all was skipped before. Skipping is
			// the same disagreement as reshaping: a proxy that rejects the line
			// and a server that ignores it read different requests.
			return parseBadReq
		}
		if i == 0 || isOWS(field[i-1]) {
			return parseBadReq
		}

		k := bytes.ToLower(field[:i])
		v := bytes.TrimSpace(field[i+1:])

		c.header = append(c.header, KV{k, v})
	}
}

// isOWS reports whether b is optional whitespace as RFC 7230 §3.2.3 defines it.
func isOWS(b byte) bool { return b == ' ' || b == '\t' }

// headerValues returns every value recorded under k. The framing headers have
// to be read as a set: header() yields only the first match, so a second
// Content-Length carrying a different length would go unnoticed.
func headerValues(c *Context, k []byte) [][]byte {
	var values [][]byte
	for i := range c.header {
		if bytes.Equal(c.header[i].K, k) {
			values = append(values, c.header[i].V)
		}
	}
	return values
}

// unescapeQuery percent-decodes a query key or value, leaving a malformed
// escape verbatim rather than dropping the pair. QueryUnescape, not
// PathUnescape: the query string is the one place where "+" spells a space.
//
// Only the query is decoded here — never the path, which the router matches on.
// Decoding "%2F" before splitting would let a client forge a segment boundary
// and reach a route the URL it sent does not name.
func unescapeQuery(v []byte) []byte {
	if bytes.IndexByte(v, '%') == -1 && bytes.IndexByte(v, '+') == -1 {
		return v
	}

	decoded, err := url.QueryUnescape(string(v))
	if err != nil {
		return v
	}

	return []byte(decoded)
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
			c.query = append(c.query, KV{unescapeQuery(pair[:eq]), unescapeQuery(pair[eq+1:])})
		} else {
			c.query = append(c.query, KV{unescapeQuery(pair), nil})
		}
	}
}

func parseBody(r *bufio.Reader, c *Context) parseResult {
	cls := headerValues(c, []byte("content-length"))
	tes := headerValues(c, []byte("transfer-encoding"))

	// RFC 7230 §3.3.3: repeated Content-Length fields whose values disagree, or
	// a Transfer-Encoding applied more than once, leave the message framing
	// undefined. Picking the first value and reading on is what lets an attacker
	// hide a second request inside the first one's body.
	if !allSameValue(cls) || len(tes) > 1 {
		return parseBadReq
	}

	// A request carrying both Content-Length and Transfer-Encoding is ambiguous
	// for the same reason — reject it outright.
	if len(cls) > 0 && len(tes) > 0 {
		return parseBadReq
	}

	if len(cls) > 0 {
		n, ok := parseContentLength(cls[0])
		if !ok {
			return parseBadReq
		}
		if n == 0 {
			return parseOK
		}
		if c.maxBodySize > 0 && n > c.maxBodySize {
			return parseTooLarge
		}
		buf := make([]byte, n)
		if _, err := io.ReadFull(r, buf); err != nil {
			return parseBadReq
		}
		c.body = buf
		return parseOK
	}

	if len(tes) > 0 {
		switch transferEncodingKind(tes[0]) {
		case teChunked:
			return parseChunked(r, c)
		case teIdentity:
			return parseOK
		default:
			// Unknown / unsupported transfer coding — reject rather than
			// silently treating the request as having no body.
			return parseBadReq
		}
	}

	return parseOK
}

// allSameValue reports whether every repetition of a field carries the same
// value. Identical repeats are harmless and RFC 7230 §3.3.3 allows collapsing
// them; differing ones are an unrecoverable framing error.
func allSameValue(values [][]byte) bool {
	for _, v := range values[min(len(values), 1):] {
		if !bytes.Equal(v, values[0]) {
			return false
		}
	}
	return true
}

// parseContentLength parses a Content-Length value. RFC 7230 §3.3.2 defines it
// as 1*DIGIT, so the digits are checked before strconv sees the value: Atoi
// also accepts a sign, and a "+5" this server reads as 5 while a strict proxy
// rejects the field entirely is a body-length disagreement.
func parseContentLength(v []byte) (int, bool) {
	if len(v) == 0 || !allDigits(v, isDigit) {
		return 0, false
	}
	n, err := strconv.Atoi(string(v))
	if err != nil {
		// Digits that overflow int.
		return 0, false
	}
	return n, true
}

// parseChunkSize parses a chunk-size, which RFC 7230 §4.1 defines as 1*HEXDIG.
// Same reasoning as parseContentLength: ParseInt would accept a signed value.
func parseChunkSize(v []byte) (int64, bool) {
	if len(v) == 0 || !allDigits(v, isHexDigit) {
		return 0, false
	}
	n, err := strconv.ParseInt(string(v), 16, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

func allDigits(v []byte, valid func(byte) bool) bool {
	for _, b := range v {
		if !valid(b) {
			return false
		}
	}
	return true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isHexDigit(b byte) bool {
	return isDigit(b) || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

type teKind int

const (
	teUnknown teKind = iota
	teIdentity
	teChunked
)

// transferEncodingKind classifies a Transfer-Encoding header value. Per
// RFC 7230 the final coding must be "chunked" for the server to delimit the
// body; matching is case-insensitive and tolerates a coding list such as
// "gzip, chunked".
func transferEncodingKind(te []byte) teKind {
	codings := bytes.Split(te, []byte(","))
	last := bytes.ToLower(bytes.TrimSpace(codings[len(codings)-1]))
	switch {
	case bytes.Equal(last, []byte("chunked")):
		return teChunked
	case bytes.Equal(last, []byte("identity")), len(last) == 0:
		return teIdentity
	default:
		return teUnknown
	}
}

// keepAlive reports whether the connection may carry another request.
// RFC 7230 §6.1 makes Connection a case-insensitive comma-separated list, so an
// exact match against "close" missed "Close" and "keep-alive, close" and left
// the connection pinned for the whole read deadline. §6.3 defaults to close
// below HTTP/1.1: a 1.0 client that did not ask for keep-alive waits for the
// close rather than sending a second request.
func keepAlive(c *Context) bool {
	if connectionHasToken(c, "close") {
		return false
	}
	if c.minorVersion == 0 {
		return connectionHasToken(c, "keep-alive")
	}
	return true
}

// connectionHasToken reports whether the Connection field carries token. The
// token may appear in any case, at any position of the comma-separated list,
// and spread over repeated Connection header lines.
func connectionHasToken(c *Context, token string) bool {
	for _, v := range headerValues(c, []byte("connection")) {
		for _, t := range bytes.Split(v, []byte(",")) {
			if bytes.EqualFold(bytes.TrimSpace(t), []byte(token)) {
				return true
			}
		}
	}
	return false
}

func parseChunked(r *bufio.Reader, c *Context) parseResult {
	var body []byte

	for {
		line, err := readLimitedLine(r, maxHeaderLineSize)
		if err != nil {
			return parseBadReq
		}

		// Strip any chunk extension (";name=value") before parsing the size.
		sizeField := bytes.TrimSpace(line)
		if i := bytes.IndexByte(sizeField, ';'); i != -1 {
			sizeField = bytes.TrimSpace(sizeField[:i])
		}

		size, ok := parseChunkSize(sizeField)
		if !ok {
			return parseBadReq
		}

		if size == 0 {
			// Consume the trailer section up to the terminating blank line.
			if !consumeChunkedTrailer(r) {
				return parseBadReq
			}
			break
		}

		// Bound the chunk size before allocating. The cumulative comparison is
		// done in int64 so that a 64-bit size never overflows int on a 32-bit
		// platform (where int(size) could wrap negative and bypass the limit).
		if c.maxBodySize > 0 && int64(len(body))+size > int64(c.maxBodySize) {
			return parseTooLarge
		}
		if size > int64(maxInt) {
			return parseTooLarge
		}

		chunk := make([]byte, size)
		if _, err := io.ReadFull(r, chunk); err != nil {
			return parseBadReq
		}
		body = append(body, chunk...)

		// Each chunk's data is terminated by a CRLF.
		if !expectCRLF(r) {
			return parseBadReq
		}
	}

	c.body = body
	return parseOK
}

// maxInt is the largest value representable by int on the build platform.
const maxInt = int(^uint(0) >> 1)

// expectCRLF reads the line terminating a chunk's data and verifies it is a
// (possibly bare-LF) empty line. A non-empty line means corrupt chunk framing.
func expectCRLF(r *bufio.Reader) bool {
	line, err := r.ReadBytes('\n')
	if err != nil {
		return false
	}
	return len(bytes.TrimRight(line, "\r\n")) == 0
}

// consumeChunkedTrailer reads optional trailer header lines after the
// last-chunk marker, stopping at the terminating blank line.
func consumeChunkedTrailer(r *bufio.Reader) bool {
	for {
		line, err := readLimitedLine(r, maxHeaderLineSize)
		if err != nil {
			return false
		}
		if len(bytes.TrimRight(line, "\r\n")) == 0 {
			return true
		}
	}
}
