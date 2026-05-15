package netio

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"testing"
	"time"
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
	app.startup()
	if !called {
		t.Error("expected startFn to be called")
	}
}

func TestStartup_WithoutStartFn(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.startFn = nil
	app.startup()
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
	if app.ln == nil {
		t.Fatal("expected listener from New()")
	}
	addr := app.ln.Addr().String()

	errCh := make(chan error, 1)
	go func() { errCh <- app.Listen() }()

	// Give it time to start, then close to stop
	time.Sleep(50 * time.Millisecond)
	app.ln.Close()

	err = <-errCh
	if err == nil {
		t.Error("expected error from closed listener")
	}
	_ = addr
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
	addr := app.ln.Addr().String()
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
	app.ln.Close()

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

	app.ln.Close()

	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error from closed listener")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ListenHTTPS did not return after close")
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