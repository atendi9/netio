package e2e

import (
	"bufio"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/atendi9/capivara/assert"
	"github.com/atendi9/netio"
	"github.com/atendi9/netio/cors"
)

// QUERY (RFC 10008) over a real socket: the hand-rolled parser is method-
// agnostic and frames the body from Content-Length, so the query content has to
// arrive intact without any method-specific handling.
func TestQueryMethodOverRawSocket(t *testing.T) {
	const origin = "https://homologaatendi9.netlify.app"

	portCh := make(chan string, 1)
	errCh := make(chan error, 1)

	app, err := netio.New(netio.AppConfig{
		Startup: func(p string) { portCh <- p },
	})
	assert.NoError(t, err)

	app.Use(cors.Middleware(cors.Config{AllowOrigins: []string{origin}}))

	var received string
	app.QUERY("/v1/search", func(c *netio.Context) {
		received = string(c.Body())
		c.JSON(map[string]any{"matched": 1})
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

	var port string
	select {
	case port = <-portCh:
	case err := <-errCh:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server startup")
	}

	const query = "SELECT * FROM tickets WHERE status = 'open'"
	conn := dialWithRetry(t, port)
	req := "QUERY /v1/search HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Origin: " + origin + "\r\n" +
		"Content-Type: application/sql\r\n" +
		fmt.Sprintf("Content-Length: %d\r\n", len(query)) +
		"\r\n" + query
	_, err = conn.Write([]byte(req))
	assert.NoError(t, err)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	statusLine, headers := readRawResponse(t, bufio.NewReader(conn))

	assert.Equal(t, "HTTP/1.1 200 OK", statusLine)
	assert.Equal(t, query, received)

	got, found := headerValue(headers, "Access-Control-Allow-Origin")
	if !found {
		t.Fatalf("QUERY response carried no CORS headers: %v", headers)
	}
	assert.Equal(t, origin, got)
}

// A QUERY with no Content-Type is rejected at the framework level per RFC 10008
// section 2, and the rejection still carries its CORS headers.
func TestQueryMethodRejectsMissingContentTypeOverSocket(t *testing.T) {
	const origin = "https://homologaatendi9.netlify.app"

	portCh := make(chan string, 1)
	app, err := netio.New(netio.AppConfig{Startup: func(p string) { portCh <- p }})
	assert.NoError(t, err)

	app.Use(cors.Middleware(cors.Config{AllowOrigins: []string{origin}}))

	reached := false
	app.QUERY("/v1/search", func(c *netio.Context) {
		reached = true
		c.SendStatus(200)
	})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		app.Shutdown(ctx)
	})
	go app.Listen()

	var port string
	select {
	case port = <-portCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server startup")
	}

	conn := dialWithRetry(t, port)
	req := "QUERY /v1/search HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Origin: " + origin + "\r\n" +
		"Content-Length: 8\r\n" +
		"\r\n" + "SELECT 1"
	_, err = conn.Write([]byte(req))
	assert.NoError(t, err)

	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	statusLine, headers := readRawResponse(t, bufio.NewReader(conn))

	assert.Equal(t, "HTTP/1.1 400 Bad Request", statusLine)
	if reached {
		t.Error("handler ran despite the missing Content-Type")
	}
	if _, found := headerValue(headers, "Access-Control-Allow-Origin"); !found {
		t.Errorf("400 lost its CORS headers: %v", headers)
	}
}
