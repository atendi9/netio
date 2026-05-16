package netio

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"
)

// parseResult indicates the outcome of parsing a request.
type parseResult int

const (
	parseOK       parseResult = 0
	parseEOF      parseResult = -1
	parseTooLarge parseResult = 413
	parseBadReq   parseResult = 400
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
	line, err := readLimitedLine(r, maxRequestLineSize)
	if err != nil {
		if err == errLineTooLong {
			return parseBadReq
		}
		return parseEOF
	}

	if !parseRequestLine(line, c) {
		return parseBadReq
	}
	if res := parseHeaders(r, c); res != parseOK {
		return res
	}

	return parseBody(r, c)
}

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

func parseRequestLine(line []byte, c *Context) bool {
	i := bytes.IndexByte(line, ' ')
	if i == -1 {
		return false
	}
	c.method = line[:i]

	rest := line[i+1:]
	j := bytes.IndexByte(rest, ' ')
	if j == -1 {
		return false
	}
	uri := rest[:j]

	if q := bytes.IndexByte(uri, '?'); q != -1 {
		c.path = uri[:q]
		parseQueryString(uri[q+1:], c)
	} else {
		c.path = uri
	}
	return true
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

		if len(bytes.TrimSpace(line)) == 0 {
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

		i := bytes.IndexByte(line, ':')
		if i == -1 {
			continue
		}
		k := bytes.ToLower(bytes.TrimSpace(line[:i]))
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

func parseBody(r *bufio.Reader, c *Context) parseResult {
	cl := header(c, []byte("content-length"))
	te := header(c, []byte("transfer-encoding"))

	// RFC 7230 §3.3.3: a request carrying both Content-Length and
	// Transfer-Encoding is ambiguous and a known request-smuggling primitive
	// behind a proxy — reject it outright.
	if cl != nil && te != nil {
		return parseBadReq
	}

	if cl != nil {
		n, err := strconv.Atoi(string(cl))
		if err != nil || n < 0 {
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

	if te != nil {
		switch transferEncodingKind(te) {
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

func keepAlive(c *Context) bool {
	v := header(c, []byte("connection"))
	return !bytes.Equal(v, []byte("close"))
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

		size, err := strconv.ParseInt(string(sizeField), 16, 64)
		if err != nil || size < 0 {
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
