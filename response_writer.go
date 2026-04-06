package netio

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (ctx *Context) writeResponseWithHeaders(
	logger Logger,
	body []byte,
) {
	if ctx.status < http.StatusContinue {
		ctx.status = http.StatusOK
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

		ctx.w.WriteHeader(ctx.status)
		if len(body) > 0 {
			ctx.w.Write(body)
		}
		ctx.wrote = true
		return
	}
	var buf bytes.Buffer

	buf.WriteString("HTTP/1.1 ")
	buf.WriteString(strconv.Itoa(ctx.status))
	buf.WriteString(" ")

	statusText := http.StatusText(ctx.status)
	if statusText == "" {
		statusText = "Unknown Status"
	}
	buf.WriteString(statusText)
	buf.WriteString("\r\n")

	hasContentType := false
	hasContentLength := false
	for _, h := range ctx.resHeader {
		key := string(h.K)
		value := string(h.V)
		if strings.EqualFold(key, "Content-Type") {
			hasContentType = true
		}
		if strings.EqualFold(key, "Content-Length") {
			hasContentLength = true
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

	if !hasContentLength {
		buf.WriteString("Content-Length: ")
		buf.WriteString(strconv.Itoa(len(body)))
		buf.WriteString("\r\n")
	}

	buf.WriteString("\r\n")

	buf.Write(body)
	responseBytes := buf.Bytes()
	logger(fmt.Sprintf("writing response: %s", string(responseBytes)))
	ctx.conn.Write(responseBytes)
	ctx.wrote = true
}
