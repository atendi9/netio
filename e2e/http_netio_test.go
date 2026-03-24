package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/atendi9/netio"
	"github.com/atendi9/netio/cors"
)

func TestNetIOHTTP(t *testing.T) {
	t.Helper()

	portCh := make(chan string, 1)
	errCh := make(chan error, 1)

	runTestOnStartup := func(p string) {
		portCh <- p
	}

	app, err := netio.New(netio.AppConfig{
		Startup: runTestOnStartup,
	})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	allowedOrigins := []string{
		"https://google.com",
		"https://atendi9.com.br",
		"https://graph.facebook.com",
	}

	app.GET("/", func(c *netio.Context) {
		c.JSON(map[string]any{"message": "Hello World"})
	})

	app.Use(cors.Middleware(cors.Config{
		AllowOrigins:  allowedOrigins,
		AllowMethods:  []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:  []string{"*"},
		ExposeHeaders: []string{"*"},
		MaxAge:        600,
	}))

	ctx, cancel := context.WithCancel(context.Background())

	t.Cleanup(func() {
		cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Errorf("shutdown error: %v", err)
		}
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

	url := fmt.Sprintf("http://localhost:%s", port)

	origin := "https://google.com"

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Origin", origin)

	var res *http.Response
	for i := 0; i < 10; i++ {
		res, err = http.DefaultClient.Do(req)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("failed to reach server: %v", err)
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", res.StatusCode)
	}

	if string(body) != `{"message":"Hello World"}` {
		t.Fatalf("unexpected body: %s", body)
	}

	if got := res.Header.Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("expected Allow-Origin %q, got %q", origin, got)
	}

	if got := res.Header.Get("Vary"); got != "Origin, Access-Control-Request-Method, Access-Control-Request-Headers" {
		t.Fatalf("expected Vary=Origin, got %q", got)
	}

	if got := res.Header.Get("Access-Control-Expose-Headers"); got != "*" {
		t.Fatalf("expected Expose-Headers *, got %q", got)
	}

	preReq, _ := http.NewRequest("OPTIONS", url, nil)
	preReq.Header.Set("Origin", origin)
	preReq.Header.Set("Access-Control-Request-Method", "POST")
	preReq.Header.Set("Access-Control-Request-Headers", "X-Test-Header")

	preRes, err := http.DefaultClient.Do(preReq)
	if err != nil {
		t.Fatalf("preflight request failed: %v", err)
	}
	defer preRes.Body.Close()

	if preRes.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", preRes.StatusCode)
	}

	if got := preRes.Header.Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("preflight: expected Allow-Origin %q, got %q", origin, got)
	}

	if got := preRes.Header.Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("missing Access-Control-Allow-Methods")
	}

	if got := preRes.Header.Get("Access-Control-Allow-Headers"); got != "X-Test-Header" {
		t.Fatalf("expected echoed headers, got %q", got)
	}

	if got := preRes.Header.Get("Access-Control-Max-Age"); got != "600" {
		t.Fatalf("expected Max-Age 600, got %q", got)
	}
}