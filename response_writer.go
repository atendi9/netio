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

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)

	buf.WriteString("HTTP/1.1 ")
	buf.WriteString(strconv.Itoa(status))
	buf.WriteByte(' ')
	buf.WriteString(http.StatusText(status))
	buf.WriteString("\r\n")

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

	// RFC 7230: MUST NOT send Content-Length with Transfer-Encoding
	if !hasContentLength && !hasTransferEncoding {
		buf.WriteString("Content-Length: ")
		buf.WriteString(strconv.Itoa(len(body)))
		buf.WriteString("\r\n")
	}

	buf.WriteString("\r\n")

	buf.Write(body)
	responseBytes := buf.Bytes()

	// Log only the headers (truncate at the blank line before body)
	if idx := bytes.Index(responseBytes, []byte("\r\n\r\n")); idx != -1 {
		logger(string(responseBytes[:idx+4]))
	}

	_, err := ctx.conn.Write(responseBytes)
	ctx.wrote = true
	return err
}
