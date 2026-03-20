package netio

import (
	"bytes"
	"net"
	"strconv"
	"strings"
)

func writeResponseWithHeaders(conn net.Conn, status int, body []byte, headers []KV) {
	var buf bytes.Buffer

	buf.WriteString("HTTP/1.1 ")
	buf.WriteString(strconv.Itoa(status))
	buf.WriteString(" OK\r\n")

	hasContentType := false
	hasContentLength := false
	for _, h := range headers {
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

	conn.Write(buf.Bytes())
}
