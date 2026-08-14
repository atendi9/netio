package netio

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atendi9/capivara/assert"
)

func TestNew_EmptyPort(t *testing.T) {
	app, err := New(AppConfig{})
	if err != nil || app.port == "" {
		t.Errorf("expected auto-assigned port, err=%v", err)
	}
	if app.ln == nil {
		t.Error("expected listener to be kept open")
	}
	app.ln.Close()
}

func TestNew_CustomLogger(t *testing.T) {
	called := false
	app, err := New(AppConfig{Port: "0", Logger: func(msgs ...string) { called = true }})
	if err != nil {
		t.Fatal(err)
	}
	app.log("x")
	if !called {
		t.Error("expected custom logger to be called")
	}
}

func TestNew_CustomAppName(t *testing.T) {
	app, err := New(AppConfig{Port: "0", AppName: "myapp"})
	if err != nil {
		t.Fatal(err)
	}
	if app.appName != "myapp" {
		t.Errorf("expected 'myapp', got %q", app.appName)
	}
}

func TestNew_DefaultMaxConns(t *testing.T) {
	app, err := New(AppConfig{Port: "0"})
	if err != nil {
		t.Fatal(err)
	}
	if cap(app.connSem) != defaultMaxConns {
		t.Errorf("expected connSem cap %d, got %d", defaultMaxConns, cap(app.connSem))
	}
}

func TestNew_CustomMaxConns(t *testing.T) {
	app, err := New(AppConfig{Port: "0", MaxConns: 5})
	if err != nil {
		t.Fatal(err)
	}
	if cap(app.connSem) != 5 {
		t.Errorf("expected connSem cap 5, got %d", cap(app.connSem))
	}
}

func TestNew_NonPositiveMaxConns(t *testing.T) {
	for _, v := range []int{0, -1} {
		app, err := New(AppConfig{Port: "0", MaxConns: v})
		if err != nil {
			t.Fatal(err)
		}
		if cap(app.connSem) != defaultMaxConns {
			t.Errorf("MaxConns=%d: expected fallback cap %d, got %d", v, defaultMaxConns, cap(app.connSem))
		}
	}
}

func TestNew_InvalidMaxBodySize(t *testing.T) {
	if _, err := New(AppConfig{Port: "0", MaxBodySize: "invalid"}); err == nil {
		t.Error("expected error for invalid MaxBodySize")
	}
}

func TestMaxBodySizeString(t *testing.T) {
	empty := MaxBodySize("").String()
	custom := MaxBodySize("20 MB").String()
	if empty != "15 MB" {
		t.Errorf("expected '15 MB', got %q", empty)
	}
	if custom != "20 MB" {
		t.Errorf("expected '20 MB', got %q", custom)
	}
}

func TestGenerateMaxBodySize_ShortString(t *testing.T) {
	if _, err := generateMaxBodySize(MaxBodySize("X")); err != ErrInvalidSize {
		t.Errorf("expected ErrInvalidSize, got %v", err)
	}
}

func TestGenerateMaxBodySize_UnknownUnit(t *testing.T) {
	if _, err := generateMaxBodySize(MaxBodySize("10PB")); err != ErrUnknownUnit {
		t.Errorf("expected ErrUnknownUnit, got %v", err)
	}
}

func TestGenerateMaxBodySize_NumberOnly(t *testing.T) {
	if _, err := generateMaxBodySize(MaxBodySize("123")); err != ErrInvalidMaxBodySizeFormat {
		t.Errorf("expected ErrInvalidMaxBodySizeFormat, got %v", err)
	}
}

func TestGenerateMaxBodySize_UnitOnly(t *testing.T) {
	if _, err := generateMaxBodySize(MaxBodySize("MB")); err != ErrInvalidMaxBodySizeFormat {
		t.Errorf("expected ErrInvalidMaxBodySizeFormat, got %v", err)
	}
}

func TestApp_Use(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.Use(func(c *Context) {})
	app.Use(func(c *Context) {})
	if len(app.mw) != 2 {
		t.Errorf("expected 2 middlewares, got %d", len(app.mw))
	}
}

func TestShutdown_NilListener(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.ln = nil
	if err := app.Shutdown(context.Background()); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestShutdown_ClosedListener(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	ln.Close()
	app.ln = ln

	err = app.Shutdown(context.Background())
	if err == nil {
		t.Error("expected error closing already-closed listener")
	}
}

func TestShutdown_ForceClosesConns(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	app.ln = ln

	// Simulate an active connection
	client, server := net.Pipe()
	defer client.Close()
	app.trackConn(server, true)
	app.activeConns.Add(1)

	// Use already-cancelled context so Shutdown hits the force-close path
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	go func() {
		time.Sleep(100 * time.Millisecond)
		app.activeConns.Done()
	}()

	app.Shutdown(ctx)

	// server should be closed by closeAllConns
	buf := make([]byte, 1)
	_, readErr := server.Read(buf)
	if readErr == nil {
		t.Error("expected error reading from force-closed connection")
	}
}

func TestStartup_WithStartFn(t *testing.T) {
	called := false
	app, _ := New(AppConfig{Port: "0", Startup: func(port string) { called = true }})
	app.startup(schemeHTTP)
	if !called {
		t.Error("expected startFn to be called")
	}
}

func TestStartup_WithoutStartFn(t *testing.T) {
	for _, scheme := range []string{schemeHTTP, schemeHTTPS} {
		t.Run(scheme, func(t *testing.T) {
			var logged []string
			app, _ := New(AppConfig{
				Port:   "8080",
				Logger: func(msgs ...string) { logged = append(logged, msgs...) },
			})
			app.startFn = nil

			app.startup(scheme)

			joined := strings.Join(logged, "")
			want := scheme + "://localhost:8080"
			if !strings.Contains(joined, want) {
				t.Errorf("banner %q does not advertise %q", joined, want)
			}
		})
	}
}

func TestServe_BodyTooLarge_BeforeAllocation(t *testing.T) {
	app, _ := New(AppConfig{Port: "0", MaxBodySize: "10B"})

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		app.serve(server)
		close(done)
	}()

	body := strings.Repeat("x", 100)
	fmt.Fprintf(client, "POST /x HTTP/1.1\r\nHost: localhost\r\ncontent-length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)

	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := client.Read(buf)
	resp := string(buf[:n])

	if !strings.Contains(resp, "413") {
		t.Errorf("expected 413 in response, got: %q", resp)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return in time")
	}
}

func TestServe_BadRequest_InvalidContentLength(t *testing.T) {
	app, _ := New(AppConfig{Port: "0", MaxBodySize: "10B"})

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		app.serve(server)
		close(done)
	}()

	fmt.Fprint(client, "POST /x HTTP/1.1\r\nHost: localhost\r\ncontent-length: abc\r\nConnection: close\r\n\r\n")

	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := client.Read(buf)
	resp := string(buf[:n])

	if !strings.Contains(resp, "400") {
		t.Errorf("expected 400 in response, got: %q", resp)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return in time")
	}
}

func TestListen_InvalidPort(t *testing.T) {
	app, _ := New(AppConfig{Port: "99999"})
	if err := app.Listen(); err == nil {
		t.Error("expected error for invalid port")
	}
}

func TestListen_ReusesExistingListener(t *testing.T) {
	app, err := New(AppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	// New() with empty port should have created a listener
	reserved := app.listener()
	if reserved == nil {
		t.Fatal("expected listener from New()")
	}
	addr := reserved.Addr().String()

	errCh := make(chan error, 1)
	go func() { errCh <- app.Listen() }()

	// Give it time to start, then close to stop
	time.Sleep(50 * time.Millisecond)
	if got := app.listener().Addr().String(); got != addr {
		t.Errorf("Listen bound %s instead of reusing %s", got, addr)
	}
	app.listener().Close()

	err = <-errCh
	if err == nil {
		t.Error("expected error from closed listener")
	}
}

func TestGenerateMaxBodySize_AllUnits(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"100B", 100},
		{"10KB", 10 << 10},
		{"15MB", 15 << 20},
		{"1GB", 1 << 30},
		{"1TB", 1 << 40},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := generateMaxBodySize(MaxBodySize(tt.input))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, got)
			}
		})
	}
}

func TestServe_RouteFound(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.GET("/hello", func(c *Context) { c.Send([]byte("world")) })

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		app.serve(server)
		close(done)
	}()

	fmt.Fprint(client, "GET /hello HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := client.Read(buf)
	resp := string(buf[:n])

	if !strings.Contains(resp, "world") {
		t.Errorf("expected 'world' in response, got: %q", resp)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return in time")
	}
}

func TestServeFiles(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(dir + "/test.txt")
	assert.NoError(t, err)
	defer f.Close()
	msg := "Hello, NetIO!"
	_, err = f.Write([]byte(msg))
	assert.NoError(t, err)

	t.Run("Path traversal vulnerability using NetIO http.Server", func(t *testing.T) {
		portChan := make(chan string, 1)

		app, _ := New(AppConfig{Startup: func(p string) {
			portChan <- p
		}})

		app.ServeFiles("/static/", dir)
		go func() {
			app.Listen()
		}()

		port := <-portChan

		url := fmt.Sprintf("http://localhost:%s/static/..%%2f..%%2fetc%%2fpasswd", port)
		res, err := http.Get(url)
		assert.NoError(t, err)
		defer res.Body.Close()
		assertEqual(t, res.StatusCode, http.StatusForbidden)
	})

	t.Run("HTTP file serving using standard http.Server", func(t *testing.T) {
		var port string = "0"
		app, _ := New(AppConfig{Startup: func(p string) {
			port = p
		}})

		app.ServeFiles("/static/", dir)
		res := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s/static/test.txt", port), nil)
		app.ServeHTTP(res, req)
		assertEqual(t, res.Code, http.StatusOK)
		assertEqual(t, res.Body.String(), msg)
	})

	t.Run("HTTP file serving using NetIO http.Server", func(t *testing.T) {
		portChan := make(chan string, 1)

		app, _ := New(AppConfig{Startup: func(p string) {
			portChan <- p
		}})

		app.ServeFiles("/static/", dir)
		go func() {
			app.Listen()
		}()

		port := <-portChan

		res, err := http.Get(fmt.Sprintf("http://localhost:%s/static/test.txt", port))
		assert.NoError(t, err)
		defer res.Body.Close()

		assertEqual(t, res.StatusCode, http.StatusOK)

		body, err := io.ReadAll(res.Body)
		assert.NoError(t, err)

		assertEqual(t, string(body), msg)
	})
}

func TestDetectContentType(t *testing.T) {
	jsonData := []byte(`{"foo":"bar"}`)
	textData := []byte("hello world")
	jsonContentType := "application/json"
	assertEqual(t, detectContentType(jsonData), jsonContentType)
	assertEqual(t, detectContentType(textData) == jsonContentType, false)
}

func TestServe_RouteNotFound(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		app.serve(server)
		close(done)
	}()

	fmt.Fprint(client, "GET /nope HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := client.Read(buf)
	resp := string(buf[:n])

	if !strings.Contains(resp, "404") {
		t.Errorf("expected 404 in response, got: %q", resp)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return in time")
	}
}

func TestServe_BodyTooLarge(t *testing.T) {
	app, _ := New(AppConfig{Port: "0", MaxBodySize: "10B"})

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		app.serve(server)
		close(done)
	}()

	body := strings.Repeat("x", 100)
	fmt.Fprintf(client, "POST /x HTTP/1.1\r\nHost: localhost\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)

	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := client.Read(buf)
	resp := string(buf[:n])

	if !strings.Contains(resp, "413") {
		t.Errorf("expected 413 in response, got: %q", resp)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return in time")
	}
}

func TestServe_EmptyReader(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})

	client, server := net.Pipe()

	done := make(chan struct{})
	go func() {
		app.serve(server)
		close(done)
	}()

	client.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return in time")
	}
}

func TestServe_WithMiddleware(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.Use(func(c *Context) {
		c.HeaderSet("X-MW", "applied")
		c.Next()
	})
	app.GET("/mw", func(c *Context) { c.Send([]byte("ok")) })

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		app.serve(server)
		close(done)
	}()

	fmt.Fprint(client, "GET /mw HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := client.Read(buf)
	resp := string(buf[:n])

	if !strings.Contains(resp, "X-MW: applied") {
		t.Errorf("expected middleware header, got: %q", resp)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return in time")
	}
}

func TestServe_HandlerNoWrite(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.GET("/noop", func(c *Context) {
		// handler that doesn't write anything — serve should send 204
	})

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		app.serve(server)
		close(done)
	}()

	fmt.Fprint(client, "GET /noop HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := client.Read(buf)
	resp := string(buf[:n])

	if !strings.Contains(resp, "204") {
		t.Errorf("expected 204 in response, got: %q", resp)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return in time")
	}
}

func TestServe_HandlerStatusWithoutBody(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.GET("/fail", func(c *Context) {
		// Handler set a status but wrote no body: the explicit status must
		// be honored rather than overridden with 204 No Content.
		c.Status(http.StatusInternalServerError)
	})

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		app.serve(server)
		close(done)
	}()

	fmt.Fprint(client, "GET /fail HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := client.Read(buf)
	resp := string(buf[:n])

	if !strings.Contains(resp, "500") {
		t.Errorf("expected 500 in response, got: %q", resp)
	}
	if strings.Contains(resp, "204") {
		t.Errorf("explicit 500 status was downgraded to 204: %q", resp)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return in time")
	}
}

func TestServe_KeepAlive(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.GET("/ping", func(c *Context) { c.Send([]byte("pong")) })

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan struct{})
	go func() {
		app.serve(server)
		close(done)
	}()

	// First request with keep-alive (default)
	fmt.Fprint(client, "GET /ping HTTP/1.1\r\nHost: localhost\r\n\r\n")

	buf := make([]byte, 4096)
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := client.Read(buf)
	if !strings.Contains(string(buf[:n]), "pong") {
		t.Errorf("expected pong in first response, got: %q", string(buf[:n]))
	}

	// Second request on same connection, close after
	fmt.Fprint(client, "GET /ping HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")

	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ = client.Read(buf)
	if !strings.Contains(string(buf[:n]), "pong") {
		t.Errorf("expected pong in second response, got: %q", string(buf[:n]))
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return in time")
	}
}

func TestListen_StartupAndShutdown(t *testing.T) {
	readyCh := make(chan struct{}, 1)
	app, _ := New(AppConfig{
		Startup: func(p string) { readyCh <- struct{}{} },
	})
	app.GET("/ping", func(c *Context) { c.Send([]byte("pong")) })

	errCh := make(chan error, 1)
	go func() { errCh <- app.Listen() }()

	select {
	case <-readyCh:
	case err := <-errCh:
		t.Fatalf("Listen failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for startup")
	}

	// Make a connection so acceptLoop's go a.serve(conn) is exercised
	addr := app.listener().Addr().String()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("failed to connect to %s: %v", addr, err)
	}
	fmt.Fprint(conn, "GET /ping HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.Read(buf)
	conn.Close()

	// Close the listener to stop Accept loop
	app.listener().Close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error from closed listener")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Listen did not return after close")
	}
}

func TestListenHTTPS_EmptyPaths(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	if err := app.ListenHTTPS("", "key.pem"); err != ErrInvalidCertKeyPaths {
		t.Errorf("expected ErrInvalidCertKeyPaths, got %v", err)
	}
	if err := app.ListenHTTPS("cert.pem", ""); err != ErrInvalidCertKeyPaths {
		t.Errorf("expected ErrInvalidCertKeyPaths, got %v", err)
	}
}

func TestListenHTTPS_InvalidCert(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	err := app.ListenHTTPS("nonexistent.crt", "nonexistent.key")
	if err == nil {
		t.Error("expected error for invalid cert files")
	}
}

func TestListenHTTPS_StartupAndShutdown(t *testing.T) {
	certPath, keyPath := generateTempCert(t)
	defer os.Remove(certPath)
	defer os.Remove(keyPath)

	portCh := make(chan string, 1)
	app, _ := New(AppConfig{
		Port:    "0",
		Startup: func(p string) { portCh <- p },
	})

	errCh := make(chan error, 1)
	go func() { errCh <- app.ListenHTTPS(certPath, keyPath) }()

	select {
	case <-portCh:
	case err := <-errCh:
		t.Fatalf("ListenHTTPS failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for startup")
	}

	app.listener().Close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error from closed listener")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ListenHTTPS did not return after close")
	}
}

// New without a port keeps its listener open to avoid a TOCTOU on the port.
// ListenHTTPS used to bind a second one on that same port and die with
// EADDRINUSE, so the auto-port path never reached the accept loop.
func TestListenHTTPS_ReusesAutoPortListener(t *testing.T) {
	certPath, keyPath := generateTempCert(t)
	defer os.Remove(certPath)
	defer os.Remove(keyPath)

	portCh := make(chan string, 1)
	app, err := New(AppConfig{Startup: func(p string) { portCh <- p }})
	if err != nil {
		t.Fatal(err)
	}
	reserved := app.port

	errCh := make(chan error, 1)
	go func() { errCh <- app.ListenHTTPS(certPath, keyPath) }()

	select {
	case port := <-portCh:
		if port != reserved {
			t.Errorf("serving on port %s, want the reserved %s", port, reserved)
		}
	case err := <-errCh:
		t.Fatalf("ListenHTTPS failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for startup")
	}

	if err := app.Shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

// Shutdown landing before Listen/ListenHTTPS publishes its listener used to
// return nil while the server went on accepting forever.
func TestShutdownBeforeBind_StopsServer(t *testing.T) {
	certPath, keyPath := generateTempCert(t)
	defer os.Remove(certPath)
	defer os.Remove(keyPath)

	listeners := map[string]func(*App) error{
		"Listen":      func(a *App) error { return a.Listen() },
		"ListenHTTPS": func(a *App) error { return a.ListenHTTPS(certPath, keyPath) },
	}

	for name, listen := range listeners {
		t.Run(name, func(t *testing.T) {
			app, _ := New(AppConfig{Port: "0", Startup: func(string) {}})

			if err := app.Shutdown(context.Background()); err != nil {
				t.Fatalf("shutdown: %v", err)
			}

			errCh := make(chan error, 1)
			go func() { errCh <- listen(app) }()

			select {
			case err := <-errCh:
				if !errors.Is(err, net.ErrClosed) {
					t.Errorf("expected net.ErrClosed, got %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("server kept accepting after Shutdown")
			}

			if ln := app.listener(); ln != nil {
				t.Errorf("listener published after Shutdown: %v", ln.Addr())
			}
		})
	}
}

func generateTempCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}

	serialNumber, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certFileObj, _ := os.CreateTemp("", "cert-*.crt")
	defer certFileObj.Close()
	keyFileObj, _ := os.CreateTemp("", "key-*.key")
	defer keyFileObj.Close()

	pem.Encode(certFileObj, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	pem.Encode(keyFileObj, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return certFileObj.Name(), keyFileObj.Name()
}

// The endpoint is normalized before the ":filename" param is appended, so an
// empty or slash-less endpoint still produces a usable route.
func TestServeFiles_NormalizesEndpoint(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/test.txt", []byte("served"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		endpoint string
		path     string
	}{
		{"empty endpoint defaults to root", "", "/test.txt"},
		{"missing trailing slash", "/static", "/static/test.txt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, _ := New(AppConfig{Port: "0"})
			if err := app.ServeFiles(tt.endpoint, dir); err != nil {
				t.Fatal(err)
			}

			res := httptest.NewRecorder()
			app.ServeHTTP(res, httptest.NewRequest(http.MethodGet, tt.path, nil))

			if res.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tt.path, res.Code)
			}
			if res.Body.String() != "served" {
				t.Errorf("body = %q, want %q", res.Body.String(), "served")
			}
		})
	}
}

// A percent-escape the router cannot decode is a malformed request, not a
// missing file: answering 404 would let a client probe with broken escapes.
// Driven over a raw socket because net/url rejects "%zz" before a net/http
// request ever reaches the app.
func TestServeFiles_UndecodableFilename(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	if err := app.ServeFiles("/static/", t.TempDir()); err != nil {
		t.Fatal(err)
	}

	got := roundTripRaw(t, app, "GET /static/%zz HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")

	if !strings.HasPrefix(got, "HTTP/1.1 400 Bad Request\r\n") {
		t.Errorf("response = %q, want a 400 status line", got)
	}
}

// OPTIONS on a path with no registered route still runs the middleware chain,
// so a CORS preflight for an unknown route is answered rather than 404'd. Both
// the raw-socket and net/http paths must agree.
func TestOptionsOnUnregisteredRoute(t *testing.T) {
	t.Run("std http", func(t *testing.T) {
		app, _ := New(AppConfig{Port: "0"})
		app.GET("/known", func(c *Context) { c.Send([]byte("ok")) })

		var sawMiddleware bool
		app.Use(func(c *Context) {
			sawMiddleware = true
			c.HeaderSet("X-Mw", "1")
			c.Next()
		})

		res := httptest.NewRecorder()
		app.ServeHTTP(res, httptest.NewRequest(http.MethodOptions, "/unknown", nil))

		if res.Code != http.StatusNoContent {
			t.Errorf("status = %d, want 204", res.Code)
		}
		if !sawMiddleware {
			t.Error("middleware skipped on unregistered preflight")
		}
		if res.Header().Get("X-Mw") != "1" {
			t.Error("middleware header dropped from the response")
		}
	})

	t.Run("raw socket", func(t *testing.T) {
		app, _ := New(AppConfig{Port: "0"})
		app.GET("/known", func(c *Context) { c.Send([]byte("ok")) })

		var sawMiddleware bool
		app.Use(func(c *Context) {
			sawMiddleware = true
			c.HeaderSet("X-Mw", "1")
			c.Next()
		})

		got := roundTripRaw(t, app, "OPTIONS /unknown HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")

		if !strings.HasPrefix(got, "HTTP/1.1 204 No Content\r\n") {
			t.Errorf("response = %q, want a 204 status line", got)
		}
		if !sawMiddleware {
			t.Error("middleware skipped on unregistered preflight")
		}
		if !strings.Contains(got, "X-Mw: 1") {
			t.Errorf("middleware header dropped: %q", got)
		}
	})
}

// roundTripRaw drives app.serve over an in-memory pipe and returns the raw
// response bytes.
func roundTripRaw(t *testing.T, app *App, request string) string {
	t.Helper()

	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		app.serve(server)
		close(done)
	}()

	client.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := client.Write([]byte(request)); err != nil {
		t.Fatalf("write request: %v", err)
	}

	var got []byte
	buf := make([]byte, 4096)
	for {
		n, err := client.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			break
		}
	}
	client.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return")
	}
	return string(got)
}

// badAddrListener reports an address net.SplitHostPort cannot parse, exercising
// the branch where the bound listener's address is unusable.
type badAddrListener struct {
	net.Listener
	closed bool
}

func (b *badAddrListener) Addr() net.Addr { return &badAddr{} }
func (b *badAddrListener) Close() error   { b.closed = true; return nil }

func TestListenHTTPS_BindErrors(t *testing.T) {
	certPath, keyPath := generateTempCert(t)
	defer os.Remove(certPath)
	defer os.Remove(keyPath)

	t.Run("port already taken", func(t *testing.T) {
		busy, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatal(err)
		}
		defer busy.Close()
		_, port, _ := net.SplitHostPort(busy.Addr().String())

		app, _ := New(AppConfig{Port: port})

		if err := app.ListenHTTPS(certPath, keyPath); err == nil {
			t.Error("expected a bind error on an occupied port")
		}
	})

	t.Run("unparseable listener address", func(t *testing.T) {
		app, _ := New(AppConfig{Port: "0"})
		ln := &badAddrListener{}
		app.setListener(ln)

		err := app.ListenHTTPS(certPath, keyPath)

		if err == nil {
			t.Fatal("expected an error for an unparseable listener address")
		}
		if !ln.closed {
			t.Error("listener leaked: it must be closed when its address is unusable")
		}
	})
}

// The numeric part is all digits by construction, but one too large for int
// still fails to convert and must be reported rather than silently truncated.
func TestGenerateMaxBodySize_NumberOverflowsInt(t *testing.T) {
	if _, err := generateMaxBodySize(MaxBodySize("99999999999999999999MB")); err != ErrInvalidMaxBodySizeFormat {
		t.Errorf("expected ErrInvalidMaxBodySizeFormat, got %v", err)
	}
}

// Each of these smuggles a second request inside the first one's body by
// exploiting a framing header the server used to reshape rather than reject.
// The server must answer 400 once, not two 200s: a second response on the
// connection is the desync itself.
func TestServe_RequestSmugglingDesync(t *testing.T) {
	smuggled := "GET /victim HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"

	tests := []struct {
		name    string
		headers string
	}{
		{
			name:    "space before the colon",
			headers: "Content-Length : 5\r\n",
		},
		{
			name:    "obs-fold continuation",
			headers: "Host: x\r\n Content-Length: 5\r\n",
		},
		{
			name:    "conflicting Content-Length",
			headers: "Content-Length: 5\r\nContent-Length: " + strconv.Itoa(5+len(smuggled)) + "\r\n",
		},
		{
			name:    "signed Content-Length",
			headers: "Content-Length: +5\r\n",
		},
		{
			name:    "signed chunk-size",
			headers: "Transfer-Encoding: chunked\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, _ := New(AppConfig{Port: "0"})
			app.POST("/echo", func(c *Context) { c.Send(c.Body()) })

			var victimHits int
			app.GET("/victim", func(c *Context) {
				victimHits++
				c.Send([]byte("victim"))
			})

			body := "hello"
			if strings.Contains(tt.headers, "chunked") {
				body = "+5\r\nhello\r\n0\r\n\r\n"
			}
			req := "POST /echo HTTP/1.1\r\n" + tt.headers + "\r\n" + body + smuggled

			got := roundTripRaw(t, app, req)

			if !strings.HasPrefix(got, "HTTP/1.1 400 Bad Request\r\n") {
				t.Errorf("response = %q, want a single 400", got)
			}
			if n := strings.Count(got, "HTTP/1.1 2"); n != 0 {
				t.Errorf("desync: %d success responses on one connection: %q", n, got)
			}
			if victimHits != 0 {
				t.Errorf("smuggled request reached its handler %d time(s)", victimHits)
			}
		})
	}
}

// A connection the server is about to close must actually close, and say so.
// Leaving it open pinned the socket for the whole read deadline and held a
// connSem slot while the client waited for bytes that were never coming.
func TestServe_ClosesConnectionWhenAsked(t *testing.T) {
	tests := []struct {
		name    string
		request string
	}{
		{"lowercase close", "GET / HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"},
		{"capitalised close", "GET / HTTP/1.1\r\nHost: x\r\nConnection: Close\r\n\r\n"},
		{"uppercase close", "GET / HTTP/1.1\r\nHost: x\r\nConnection: CLOSE\r\n\r\n"},
		{"close inside a list", "GET / HTTP/1.1\r\nHost: x\r\nConnection: keep-alive, close\r\n\r\n"},
		{"HTTP/1.0 without Connection", "GET / HTTP/1.0\r\nHost: x\r\n\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, _ := New(AppConfig{Port: "0"})
			app.GET("/", func(c *Context) { c.Send([]byte("ok")) })

			// roundTripRaw only returns once serve() has returned, so reaching
			// this line at all means the connection was not held open.
			got := roundTripRaw(t, app, tt.request)

			if !strings.HasPrefix(got, "HTTP/1.1 200 OK\r\n") {
				t.Fatalf("response = %q", got)
			}
			if !strings.Contains(got, "Connection: close\r\n") {
				t.Errorf("server closed without announcing it: %q", got)
			}
		})
	}
}

// An HTTP/1.1 client that says nothing about Connection keeps its connection,
// so a second request on the same socket is answered.
func TestServe_KeepsHTTP11ConnectionOpen(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.GET("/", func(c *Context) { c.Send([]byte("ok")) })

	req := "GET / HTTP/1.1\r\nHost: x\r\n\r\n" +
		"GET / HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"
	got := roundTripRaw(t, app, req)

	if n := strings.Count(got, "HTTP/1.1 200 OK"); n != 2 {
		t.Errorf("got %d responses, want 2: %q", n, got)
	}
	if n := strings.Count(got, "Connection: close"); n != 1 {
		t.Errorf("Connection: close appeared %d times, want 1 (only the final response): %q", n, got)
	}
}

// RFC 7230 §2.6: an unsupported major version is 505, not 400, and garbage in
// the version token is not a servable request at all.
func TestServe_RequestLineVersion(t *testing.T) {
	tests := []struct {
		name    string
		request string
		want    string
	}{
		{"HTTP/2.0", "GET / HTTP/2.0\r\nHost: x\r\n\r\n", "HTTP/1.1 505 HTTP Version Not Supported\r\n"},
		{"HTTP/0.9", "GET / HTTP/0.9\r\nHost: x\r\n\r\n", "HTTP/1.1 505 HTTP Version Not Supported\r\n"},
		{"garbage version", "GET / JUNK\r\nHost: x\r\n\r\n", "HTTP/1.1 400 Bad Request\r\n"},
		{"junk after version", "GET / HTTP/1.1 extra\r\nHost: x\r\n\r\n", "HTTP/1.1 400 Bad Request\r\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, _ := New(AppConfig{Port: "0"})
			app.GET("/", func(c *Context) { c.Send([]byte("ok")) })

			got := roundTripRaw(t, app, tt.request)

			if !strings.HasPrefix(got, tt.want) {
				t.Errorf("response = %q, want prefix %q", got, tt.want)
			}
		})
	}
}

// RFC 7231 §4.3.2: HEAD is GET without the body. Every GET route must answer it
// — 404ing HEAD breaks health checks and link checkers — and the response has to
// keep the headers describing the body it withholds.
func TestHEAD(t *testing.T) {
	newApp := func() *App {
		app, _ := New(AppConfig{Port: "0"})
		app.GET("/users/:id", func(c *Context) {
			c.JSON(map[string]any{"id": c.Param("id")})
		})
		return app
	}

	const wantBody = `{"id":"7"}`

	t.Run("raw socket falls back to the GET handler", func(t *testing.T) {
		got := roundTripRaw(t, newApp(), "HEAD /users/7 HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")

		if !strings.HasPrefix(got, "HTTP/1.1 200 OK\r\n") {
			t.Fatalf("response = %q", got)
		}
		if !strings.Contains(got, "Content-Length: "+strconv.Itoa(len(wantBody))+"\r\n") {
			t.Errorf("Content-Length does not describe the withheld body: %q", got)
		}
		if !strings.Contains(got, "Content-Type: application/json\r\n") {
			t.Errorf("Content-Type missing: %q", got)
		}
		if i := strings.Index(got, "\r\n\r\n"); i == -1 || got[i+4:] != "" {
			t.Errorf("HEAD response carries a body: %q", got)
		}
	})

	t.Run("std http falls back to the GET handler", func(t *testing.T) {
		res := httptest.NewRecorder()
		newApp().ServeHTTP(res, httptest.NewRequest(http.MethodHead, "/users/7", nil))

		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", res.Code)
		}
		if got := res.Header().Get("Content-Length"); got != strconv.Itoa(len(wantBody)) {
			t.Errorf("Content-Length = %q, want %d", got, len(wantBody))
		}
		if res.Body.Len() != 0 {
			t.Errorf("HEAD response carries a body: %q", res.Body.String())
		}
	})

	t.Run("GET is unaffected", func(t *testing.T) {
		got := roundTripRaw(t, newApp(), "GET /users/7 HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")
		if !strings.HasSuffix(got, "\r\n\r\n"+wantBody) {
			t.Errorf("GET body = %q, want it to end with %q", got, wantBody)
		}
	})

	t.Run("explicit HEAD route wins and still sends no body", func(t *testing.T) {
		app := newApp()
		app.HEAD("/users/:id", func(c *Context) {
			c.HeaderSet("X-Explicit", "1")
			c.Send([]byte("should not be sent"))
		})

		got := roundTripRaw(t, app, "HEAD /users/7 HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")

		if !strings.Contains(got, "X-Explicit: 1\r\n") {
			t.Errorf("explicit HEAD route did not run: %q", got)
		}
		if i := strings.Index(got, "\r\n\r\n"); i == -1 || got[i+4:] != "" {
			t.Errorf("HEAD response carries a body: %q", got)
		}
	})

	t.Run("unknown route still 404s", func(t *testing.T) {
		got := roundTripRaw(t, newApp(), "HEAD /nope HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")
		if !strings.HasPrefix(got, "HTTP/1.1 404 Not Found\r\n") {
			t.Errorf("response = %q, want 404", got)
		}
	})

	// The withheld body must not desync the connection: a Content-Length with
	// no bytes behind it has to leave the next pipelined request readable.
	t.Run("keep-alive framing survives", func(t *testing.T) {
		req := "HEAD /users/7 HTTP/1.1\r\nHost: x\r\n\r\n" +
			"GET /users/7 HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"
		got := roundTripRaw(t, newApp(), req)

		if n := strings.Count(got, "HTTP/1.1 200 OK"); n != 2 {
			t.Fatalf("got %d responses, want 2: %q", n, got)
		}
		if n := strings.Count(got, wantBody); n != 1 {
			t.Errorf("body appeared %d times, want 1 (only the GET): %q", n, got)
		}
	})
}

// A real client must be able to HEAD the server without hanging on a body the
// Content-Length promises but HEAD forbids.
func TestHEAD_RealClient(t *testing.T) {
	_, port := listeningApp(t, func(a *App) {
		a.GET("/", func(c *Context) { c.Send([]byte("hello")) })
	})

	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()

	res, err := client.Head("http://127.0.0.1:" + port + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", res.StatusCode)
	}
	if res.ContentLength != int64(len("hello")) {
		t.Errorf("ContentLength = %d, want %d", res.ContentLength, len("hello"))
	}
	body, _ := io.ReadAll(res.Body)
	if len(body) != 0 {
		t.Errorf("body = %q, want empty", body)
	}
}

// RFC 7231 §5.1.1: a client that sent Expect: 100-continue holds the body back
// until the interim response arrives, so never sending it costs the request a
// stall; an expectation the server cannot meet is 417, not a header to skip.
func TestServe_Expect100Continue(t *testing.T) {
	t.Run("interim response precedes the real one", func(t *testing.T) {
		app, _ := New(AppConfig{Port: "0"})
		app.POST("/echo", func(c *Context) { c.Send(c.Body()) })

		req := "POST /echo HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\n" +
			"Expect: 100-continue\r\nConnection: close\r\n\r\nhello"
		got := roundTripRaw(t, app, req)

		if !strings.HasPrefix(got, "HTTP/1.1 100 Continue\r\n\r\n") {
			t.Fatalf("no interim response: %q", got)
		}
		rest := strings.TrimPrefix(got, "HTTP/1.1 100 Continue\r\n\r\n")
		if !strings.HasPrefix(rest, "HTTP/1.1 200 OK\r\n") || !strings.HasSuffix(rest, "\r\n\r\nhello") {
			t.Errorf("final response = %q", rest)
		}
	})

	t.Run("unmeetable expectation is 417", func(t *testing.T) {
		app, _ := New(AppConfig{Port: "0"})
		app.POST("/echo", func(c *Context) { c.Send(c.Body()) })

		req := "POST /echo HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\n" +
			"Expect: the-impossible\r\nConnection: close\r\n\r\nhello"
		got := roundTripRaw(t, app, req)

		if !strings.HasPrefix(got, "HTTP/1.1 417 Expectation Failed\r\n") {
			t.Errorf("response = %q, want 417", got)
		}
	})

	t.Run("HTTP/1.0 gets no interim response", func(t *testing.T) {
		app, _ := New(AppConfig{Port: "0"})
		app.POST("/echo", func(c *Context) { c.Send(c.Body()) })

		req := "POST /echo HTTP/1.0\r\nHost: x\r\nContent-Length: 5\r\n" +
			"Expect: 100-continue\r\n\r\nhello"
		got := roundTripRaw(t, app, req)

		if strings.Contains(got, "100 Continue") {
			t.Errorf("HTTP/1.0 client sent an interim response: %q", got)
		}
		if !strings.HasPrefix(got, "HTTP/1.1 200 OK\r\n") {
			t.Errorf("response = %q", got)
		}
	})
}

// Port "0" asks the OS to pick. Listen used to leave a.port as the literal "0",
// so the startup callback reported a port nothing was listening on.
func TestListen_ResolvesAutoPort(t *testing.T) {
	app, port := listeningApp(t, func(a *App) {
		a.GET("/", func(c *Context) { c.Send([]byte("ok")) })
	})

	if port == "0" || port == "" {
		t.Fatalf("startup reported port %q", port)
	}
	if got := app.listener().Addr().String(); !strings.HasSuffix(got, ":"+port) {
		t.Errorf("listening on %s but reported port %s", got, port)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()

	res, err := client.Get("http://127.0.0.1:" + port + "/")
	if err != nil {
		t.Fatalf("reported port is not reachable: %v", err)
	}
	res.Body.Close()
}

// listeningApp starts an app on an OS-assigned port and returns it with the
// port the startup callback reported. Shutdown runs with a deadline so an idle
// keep-alive connection cannot stall cleanup until the read deadline fires.
func listeningApp(t *testing.T, reg func(*App)) (*App, string) {
	t.Helper()

	portCh := make(chan string, 1)
	app, err := New(AppConfig{Port: "0", Startup: func(p string) { portCh <- p }})
	if err != nil {
		t.Fatal(err)
	}
	reg(app)

	go app.Listen()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		app.Shutdown(ctx)
	})

	select {
	case port := <-portCh:
		return app, port
	case <-time.After(3 * time.Second):
		t.Fatal("no startup callback")
	}
	return nil, ""
}

// Routes registered through a group answer HEAD by the same GET fallback, with
// the group's middleware chain intact.
func TestHEAD_OnGroupRoute(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})

	var mwRan bool
	g := app.Group("/api", func(c *Context) {
		mwRan = true
		c.HeaderSet("X-Group", "1")
		c.Next()
	})
	g.Get("/ping", func(c *Context) { c.Send([]byte("pong")) })

	got := roundTripRaw(t, app, "HEAD /api/ping HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n")

	if !strings.HasPrefix(got, "HTTP/1.1 200 OK\r\n") {
		t.Fatalf("response = %q", got)
	}
	if !mwRan || !strings.Contains(got, "X-Group: 1\r\n") {
		t.Errorf("group middleware did not run: %q", got)
	}
	if !strings.Contains(got, "Content-Length: 4\r\n") {
		t.Errorf("Content-Length does not describe the withheld body: %q", got)
	}
	if i := strings.Index(got, "\r\n\r\n"); i == -1 || got[i+4:] != "" {
		t.Errorf("HEAD response carries a body: %q", got)
	}
}

// A peer that hangs up while waiting for the interim response must end the
// connection, not be answered into a dead socket.
func TestServe_Expect100Continue_WriteFails(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.POST("/echo", func(c *Context) { c.Send(c.Body()) })

	req := "POST /echo HTTP/1.1\r\nHost: x\r\nContent-Length: 5\r\nExpect: 100-continue\r\n\r\nhello"
	conn := &readOnlyConn{r: strings.NewReader(req)}

	done := make(chan struct{})
	go func() {
		app.serve(conn)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serve kept going after the interim response failed to send")
	}
}

// The client sends "ApiKey"; the parser lowercases it; the handler binds it with
// a tag written `header:"apiKey"`. Matching the tag verbatim broke that last
// step, and the handler saw an empty key on every authenticated request.
func TestServe_MixedCaseHeaderReachesHandler(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.Group("/v1").Get("/health", func(c *Context) {
		var header struct {
			ApiKey string `header:"apiKey"`
		}
		if err := c.ReqHeaderParser(&header); err != nil {
			t.Errorf("ReqHeaderParser: %v", err)
		}
		c.Send([]byte(header.ApiKey + "|" + c.Header("apiKey")))
	})

	resp := serveRequest(t, app,
		"GET /v1/health HTTP/1.1\r\nHost: x\r\nApiKey: secret\r\nConnection: close\r\n\r\n")

	if !strings.Contains(resp, "secret|secret") {
		t.Errorf("handler did not receive the ApiKey header:\n%s", resp)
	}
}
