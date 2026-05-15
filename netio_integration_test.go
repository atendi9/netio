//go:build integration

package netio

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func serveOneRequest(t *testing.T, app *App, request string) string {
	t.Helper()
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	done := make(chan struct{})

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		app.serve(conn)
		close(done)
	}()

	conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	fmt.Fprint(conn, request)

	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _ := conn.Read(buf)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("serve did not return in time")
	}

	return string(buf[:n])
}

func TestServe_WithRoute_Integration(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.GET("/hello", func(c *Context) { c.Send([]byte("world")) })
	resp := serveOneRequest(t, app, "GET /hello HTTP/1.1\r\nHost: localhost\r\nConnection: close\r\n\r\n")
	if !strings.Contains(resp, "world") {
		t.Errorf("expected 'world' in response, got: %q", resp)
	}
}
