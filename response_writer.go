package netio

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

func (ctx *Context) writeResponseWithHeaders(
	logger Logger,
	status int,
	body []byte,
) error {
	if ctx.wrote {
		return nil
	}

	if status < http.StatusContinue {
		status = http.StatusOK
	}

	if ctx.isStdHTTP {
		hasContentType := false
		for _, h := range ctx.resHeader {
			key := string(h.K)
			if strings.EqualFold(key, "Content-Type") {
				hasContentType = true
			}
			ctx.w.Header().Add(key, string(h.V))
		}

		if !hasContentType && len(body) > 0 {
			ctx.w.Header().Set("Content-Type", detectContentType(body))
		}

		// net/http drops a HEAD response's body but will not invent the
		// Content-Length the discarded body would have had, so set it here.
		if ctx.suppressBody && !isBodylessStatus(status) {
			ctx.w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}

		ctx.w.WriteHeader(status)
		if len(body) > 0 && !ctx.suppressBody {
			ctx.w.Write(body)
		}
		ctx.wrote = true
		return nil
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	statusText := http.StatusText(status)
	if statusText == "" {
		statusText = "Unknown Status"
	}

	buf.WriteString("HTTP/1.1 ")
	buf.WriteString(strconv.Itoa(status))
	buf.WriteByte(' ')
	buf.WriteString(statusText)
	buf.WriteString("\r\n")

	bodyless := isBodylessStatus(status)

	hasContentType := false
	hasContentLength := false
	hasTransferEncoding := false
	for _, h := range ctx.resHeader {
		key := string(h.K)
		value := string(h.V)
		if strings.EqualFold(key, "Content-Type") {
			hasContentType = true
		}
		if strings.EqualFold(key, "Content-Length") {
			// RFC 7230 §3.3.2 forbids Content-Length on 1xx and 204 outright,
			// so a caller-set one is dropped rather than forwarded.
			if bodyless {
				continue
			}
			hasContentLength = true
		}
		if strings.EqualFold(key, "Transfer-Encoding") {
			hasTransferEncoding = true
		}
		buf.WriteString(key)
		buf.WriteString(": ")
		buf.WriteString(value)
		buf.WriteString("\r\n")
	}

	if !hasContentType && len(body) > 0 {
		contentType := detectContentType(body)
		buf.WriteString("Content-Type: ")
		buf.WriteString(contentType)
		buf.WriteString("\r\n")
	}

	// RFC 7230 §3.3.2: MUST NOT send Content-Length with Transfer-Encoding, nor
	// on a status that defines no body (1xx, 204). CORS preflights answer 204,
	// so emitting it there put a spec violation on every preflight response.
	if !hasContentLength && !hasTransferEncoding && !bodyless {
		buf.WriteString("Content-Length: ")
		buf.WriteString(strconv.Itoa(len(body)))
		buf.WriteString("\r\n")
	}

	headerBlock := buf.String()

	buf.WriteString("\r\n")
	// The Content-Length above still describes the body, which is exactly what
	// a HEAD response must report while sending none of it.
	if !ctx.suppressBody {
		buf.Write(body)
	}
	responseBytes := buf.Bytes()

	// Log the status/header block with sensitive header values redacted, so
	// tokens and cookies are never written to logs.
	logger(redactSensitiveHeaders(headerBlock))

	_, err := ctx.conn.Write(responseBytes)
	ctx.wrote = true
	return err
}

// isBodylessStatus reports whether the status code defines no message body, per
// RFC 7230 §3.3.2. 304 is excluded on purpose: its Content-Length describes the
// body the equivalent 200 would have carried, so it stays valid there.
func isBodylessStatus(status int) bool {
	return status < http.StatusOK || status == http.StatusNoContent
}

// sensitiveResponseHeaders are header names whose values must not be logged.
var sensitiveResponseHeaders = map[string]struct{}{
	"set-cookie":          {},
	"authorization":       {},
	"proxy-authorization": {},
	"www-authenticate":    {},
	"proxy-authenticate":  {},
	"cookie":              {},
}

// redactSensitiveHeaders replaces the values of sensitive headers in a raw
// header block with "[REDACTED]", leaving the rest of the block intact.
func redactSensitiveHeaders(block string) string {
	lines := strings.Split(block, "\r\n")
	for i, line := range lines {
		colon := strings.IndexByte(line, ':')
		if colon == -1 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(line[:colon]))
		if _, ok := sensitiveResponseHeaders[name]; ok {
			lines[i] = line[:colon+1] + " [REDACTED]"
		}
	}
	return strings.Join(lines, "\r\n")
}
