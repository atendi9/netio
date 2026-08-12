package e2e

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/atendi9/capivara/assert"
	"github.com/atendi9/netio"
	"github.com/atendi9/netio/cors"
)

// RFC 7230 §3.3.2 forbids Content-Length on a 204, and every CORS preflight is a
// 204 — so the violation landed on every preflight the server answered.
func TestPreflightOmitsContentLength(t *testing.T) {
	const origin = "https://homologaatendi9.netlify.app"
	port := startPreflightServer(t, origin)

	conn := dialWithRetry(t, port)
	_, err := conn.Write([]byte(preflightRequest("/v1/dashboard/test@test.com/all", origin)))
	assert.NoError(t, err)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	statusLine, headers := readRawResponse(t, bufio.NewReader(conn))

	assert.Equal(t, "HTTP/1.1 204 No Content", statusLine)

	if value, found := headerValue(headers, "Content-Length"); found {
		t.Errorf("204 preflight must not carry Content-Length, got %q (headers: %v)", value, headers)
	}
	if _, found := headerValue(headers, "Transfer-Encoding"); found {
		t.Errorf("204 preflight must not carry Transfer-Encoding (headers: %v)", headers)
	}
}

// A bodyless 204 is self-delimiting, so dropping Content-Length must not break
// connection reuse: the next request on the same socket has to be answered.
func TestPreflightKeepsConnectionUsable(t *testing.T) {
	const origin = "https://homologaatendi9.netlify.app"
	port := startPreflightServer(t, origin)

	conn := dialWithRetry(t, port)
	reader := bufio.NewReader(conn)

	_, err := conn.Write([]byte(preflightRequest("/v1/dashboard/test@test.com/all", origin)))
	assert.NoError(t, err)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	statusLine, _ := readRawResponse(t, reader)
	assert.Equal(t, "HTTP/1.1 204 No Content", statusLine)

	// Same connection, real request following the preflight.
	get := "GET /v1/dashboard/test@test.com/all HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Origin: " + origin + "\r\n" +
		"\r\n"
	_, err = conn.Write([]byte(get))
	assert.NoError(t, err)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	statusLine, headers := readRawResponse(t, reader)
	assert.Equal(t, "HTTP/1.1 200 OK", statusLine)

	got, found := headerValue(headers, "Access-Control-Allow-Origin")
	if !found {
		t.Fatalf("GET after preflight lost its CORS headers: %v", headers)
	}
	assert.Equal(t, origin, got)

	length, found := headerValue(headers, "Content-Length")
	if !found {
		t.Fatalf("200 with a body must carry Content-Length: %v", headers)
	}
	assert.Equal(t, fmt.Sprint(len(`{"message":"ok"}`)), length)
}

// The full header set a browser needs before it will release the real request.
func TestPreflightEmitsCorsHeaders(t *testing.T) {
	const origin = "https://homologaatendi9.netlify.app"
	port := startPreflightServer(t, origin)

	conn := dialWithRetry(t, port)
	_, err := conn.Write([]byte(preflightRequest("/v1/dashboard/test@test.com/all", origin)))
	assert.NoError(t, err)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, headers := readRawResponse(t, bufio.NewReader(conn))

	expected := map[string]string{
		"Access-Control-Allow-Origin":      origin,
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Allow-Methods":     "GET, POST, PUT, DELETE, PATCH",
		// The configured list, not an echo of Access-Control-Request-Headers:
		// AllowHeaders is set and does not contain "*".
		"Access-Control-Allow-Headers": "apikey, authorization",
		"Access-Control-Max-Age":       "3600",
		"Vary":                         "Origin, Access-Control-Request-Method, Access-Control-Request-Headers",
	}
	for name, want := range expected {
		got, found := headerValue(headers, name)
		if !found {
			t.Errorf("missing %s (headers: %v)", name, headers)
			continue
		}
		assert.Equal(t, want, got)
	}
}

// Regression guard for the original production failure: API_CORS holds several
// origins in one comma-separated string, with incidental whitespace and a
// trailing slash. Before normalization this matched nothing and every preflight
// came back as a bare 204 with no CORS headers at all.
func TestPreflightWithCommaSeparatedOrigins(t *testing.T) {
	const apiCors = "https://homologaatendi9.netlify.app, https://app.atendi9.com/"
	port := startPreflightServer(t, apiCors)

	for _, origin := range []string{
		"https://homologaatendi9.netlify.app",
		"https://app.atendi9.com",
	} {
		t.Run(origin, func(t *testing.T) {
			conn := dialWithRetry(t, port)
			_, err := conn.Write([]byte(preflightRequest("/v1/dashboard/test@test.com/all", origin)))
			assert.NoError(t, err)

			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			statusLine, headers := readRawResponse(t, bufio.NewReader(conn))

			assert.Equal(t, "HTTP/1.1 204 No Content", statusLine)
			got, found := headerValue(headers, "Access-Control-Allow-Origin")
			if !found {
				t.Fatalf("no CORS headers for allowed origin %q: %v", origin, headers)
			}
			assert.Equal(t, origin, got)
		})
	}
}

// An origin outside the list still gets no CORS headers — normalization must not
// have loosened matching into a substring check.
func TestPreflightRejectsForeignOrigin(t *testing.T) {
	port := startPreflightServer(t, "https://homologaatendi9.netlify.app")

	for _, origin := range []string{
		"https://homologaatendi9.netlify.app.evil.com",
		"http://homologaatendi9.netlify.app",
		"https://evil.com",
	} {
		t.Run(origin, func(t *testing.T) {
			conn := dialWithRetry(t, port)
			_, err := conn.Write([]byte(preflightRequest("/v1/dashboard/test@test.com/all", origin)))
			assert.NoError(t, err)

			conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			statusLine, headers := readRawResponse(t, bufio.NewReader(conn))

			assert.Equal(t, "HTTP/1.1 204 No Content", statusLine)
			if value, found := headerValue(headers, "Access-Control-Allow-Origin"); found {
				t.Errorf("foreign origin %q was allowed: %s", origin, value)
			}
		})
	}
}

// The status codes that do define a body must keep Content-Length.
func TestNonBodylessStatusKeepsContentLength(t *testing.T) {
	port := startPreflightServer(t, "https://homologaatendi9.netlify.app")

	conn := dialWithRetry(t, port)
	// No route registered for this path, so the server answers 404.
	req := "GET /does/not/exist HTTP/1.1\r\nHost: localhost\r\n\r\n"
	_, err := conn.Write([]byte(req))
	assert.NoError(t, err)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	statusLine, headers := readRawResponse(t, bufio.NewReader(conn))

	assert.Equal(t, fmt.Sprintf("HTTP/1.1 %d %s", http.StatusNotFound, http.StatusText(http.StatusNotFound)), statusLine)
	if _, found := headerValue(headers, "Content-Length"); !found {
		t.Errorf("404 must still carry Content-Length: %v", headers)
	}
}

// startPreflightServer boots a netio app whose CORS config mirrors how it is
// wired in production: the allowed origins arrive as a single comma-separated
// environment string rather than a pre-split slice.
func startPreflightServer(t *testing.T, apiCors string) string {
	t.Helper()

	portCh := make(chan string, 1)
	errCh := make(chan error, 1)

	app, err := netio.New(netio.AppConfig{
		Startup: func(p string) { portCh <- p },
	})
	assert.NoError(t, err)

	app.Use(cors.Middleware(cors.Config{
		AllowOrigins:     []string{apiCors},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
		AllowHeaders:     []string{"apikey", "authorization"},
		MaxAge:           3600,
		AllowCredentials: true,
	}))

	app.GET("/v1/dashboard/:gmail/all", func(c *netio.Context) {
		c.JSON(map[string]any{"message": "ok"})
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		app.Shutdown(ctx)
	})

	go func() {
		if err := app.Listen(); err != nil {
			errCh <- err
		}
	}()

	select {
	case port := <-portCh:
		return port
	case err := <-errCh:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server startup")
	}

	return ""
}

// dialWithRetry opens a TCP connection, tolerating the brief window between the
// startup callback firing and the listener accepting.
func dialWithRetry(t *testing.T, port string) net.Conn {
	t.Helper()

	var conn net.Conn
	var err error
	for range 10 {
		conn, err = net.Dial("tcp", "localhost:"+port)
		if err == nil {
			t.Cleanup(func() { conn.Close() })
			return conn
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("could not connect to server: %v", err)
	return nil
}

func preflightRequest(path, origin string) string {
	return "OPTIONS " + path + " HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Origin: " + origin + "\r\n" +
		"Access-Control-Request-Method: GET\r\n" +
		"Access-Control-Request-Headers: apikey,authorization\r\n" +
		"\r\n"
}

// readRawResponse reads one full HTTP response off the wire and returns the raw
// status line plus header block. Parsing by hand is deliberate: net/http
// synthesizes Content-Length on the client side, so it cannot prove whether the
// header was actually transmitted.
func readRawResponse(t *testing.T, r *bufio.Reader) (statusLine string, headers []string) {
	t.Helper()

	line, err := r.ReadString('\n')
	assert.NoError(t, err)
	statusLine = strings.TrimRight(line, "\r\n")

	for {
		line, err := r.ReadString('\n')
		assert.NoError(t, err)
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return statusLine, headers
		}
		headers = append(headers, line)
	}
}

func headerValue(headers []string, name string) (string, bool) {
	for _, h := range headers {
		key, value, found := strings.Cut(h, ":")
		if found && strings.EqualFold(strings.TrimSpace(key), name) {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}
