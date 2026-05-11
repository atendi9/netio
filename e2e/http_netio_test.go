package e2e

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/atendi9/capivara/assert"
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
	assert.NoError(t, err)

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
		err := app.Shutdown(ctx)
		assert.NoError(t, err)
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
	assert.NoError(t, err)
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, `{"message":"Hello World"}`, string(body))
	got := res.Header.Get("Access-Control-Allow-Origin")
	assert.Equal(t, origin, got)
	got = res.Header.Get("Vary")
	assert.Equal(t, "Origin", got)
	got = res.Header.Get("Access-Control-Expose-Headers")
	assert.Equal(t, "*", got)


	preReq, _ := http.NewRequest("OPTIONS", url, nil)
	preReq.Header.Set("Origin", origin)
	preReq.Header.Set("Access-Control-Request-Method", "POST")
	preReq.Header.Set("Access-Control-Request-Headers", "X-Test-Header")

	preRes, err := http.DefaultClient.Do(preReq)
	assert.NoError(t, err)
	defer preRes.Body.Close()
	assert.Equal(t, http.StatusNoContent, preRes.StatusCode)
	got = preRes.Header.Get("Access-Control-Allow-Origin")
	assert.Equal(t, origin ,got)
	got = preRes.Header.Get("Access-Control-Allow-Methods")
	assert.Equal(t, "GET, POST, PUT, DELETE, PATCH, OPTIONS", got)
	got = preRes.Header.Get("Access-Control-Allow-Headers")
	assert.Equal(t, "X-Test-Header", got)
	got = preRes.Header.Get("Access-Control-Max-Age")
	assert.Equal(t, "600", got)
}

func TestAtendi9CORSConfig(t *testing.T) {
	portCh := make(chan string, 1)
	errCh := make(chan error, 1)

	app, err := netio.New(netio.AppConfig{
		Startup: func(p string) { portCh <- p },
	})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	allowedOrigin := "https://example.com"

	app.Use(func(c *netio.Context) {
		_ = c.Header("X-Forwarded-For")
		_ = c.Method()
	})

	app.Use(cors.Middleware(cors.Config{
		AllowOrigins:     []string{allowedOrigin},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"*"},
		ExposeHeaders:    []string{"*"},
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

	var port string
	select {
	case port = <-portCh:
	case err := <-errCh:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server startup")
	}

	baseURL := fmt.Sprintf("http://localhost:%s", port)

	t.Run("preflight", func(t *testing.T) {
		req, _ := http.NewRequest("OPTIONS", baseURL+"/v1/dashboard/test@test.com/all", nil)
		req.Header.Set("Origin", allowedOrigin)
		req.Header.Set("Access-Control-Request-Method", "GET")
		req.Header.Set("Access-Control-Request-Headers", "apikey,authorization")

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer res.Body.Close()

		if res.StatusCode != 204 {
			t.Fatalf("expected 204, got %d", res.StatusCode)
		}

		if got := res.Header.Get("Access-Control-Allow-Origin"); got != allowedOrigin {
			t.Fatalf("expected Allow-Origin %q, got %q", allowedOrigin, got)
		}

		if got := res.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Fatalf("expected Allow-Credentials true, got %q", got)
		}

		if got := res.Header.Get("Access-Control-Allow-Methods"); got == "" {
			t.Fatal("missing Access-Control-Allow-Methods")
		}

		if got := res.Header.Get("Access-Control-Allow-Headers"); got != "apikey,authorization" {
			t.Fatalf("expected echoed headers, got %q", got)
		}
	})

	t.Run("get_with_origin", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/v1/dashboard/test@test.com/all", nil)
		req.Header.Set("Origin", allowedOrigin)

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer res.Body.Close()

		if res.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", res.StatusCode)
		}

		if got := res.Header.Get("Access-Control-Allow-Origin"); got != allowedOrigin {
			t.Fatalf("expected Allow-Origin %q, got %q", allowedOrigin, got)
		}

		if got := res.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Fatalf("expected Allow-Credentials true, got %q", got)
		}
	})

	t.Run("get_with_query_params", func(t *testing.T) {
		req, _ := http.NewRequest("GET", baseURL+"/v1/dashboard/test@test.com/all?startDate=26/03/2026%2008:00&endDate=26/03/2026%2018:00&duration=seconds", nil)
		req.Header.Set("Origin", allowedOrigin)

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer res.Body.Close()

		if got := res.Header.Get("Access-Control-Allow-Origin"); got != allowedOrigin {
			t.Fatalf("expected Allow-Origin %q, got %q", allowedOrigin, got)
		}
	})
}