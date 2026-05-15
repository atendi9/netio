package cors_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/atendi9/netio"
	"github.com/atendi9/netio/cors"
)

func startServer(t *testing.T, cfg cors.Config) string {
	t.Helper()

	portCh := make(chan string, 1)
	errCh := make(chan error, 1)

	app, err := netio.New(netio.AppConfig{
		Startup: func(p string) { portCh <- p },
	})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	app.Use(cors.Middleware(cfg))
	app.GET("/", func(c *netio.Context) { c.SendStatus(200) })

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
		return fmt.Sprintf("http://localhost:%s", port)
	case err := <-errCh:
		t.Fatalf("server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server startup")
	}

	return ""
}

func get(t *testing.T, url, origin string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func preflight(t *testing.T, url, origin, method, headers string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("OPTIONS", url, nil)
	req.Header.Set("Origin", origin)
	req.Header.Set("Access-Control-Request-Method", method)
	if headers != "" {
		req.Header.Set("Access-Control-Request-Headers", headers)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("preflight request failed: %v", err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func assertHeader(t *testing.T, res *http.Response, key, expected string) {
	t.Helper()
	if got := res.Header.Get(key); got != expected {
		t.Fatalf("header %q: expected %q, got %q", key, expected, got)
	}
}

func TestOriginNotAllowed(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"https://allowed.com"},
	})

	res := get(t, url, "https://notallowed.com")

	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	assertHeader(t, res, "Access-Control-Allow-Origin", "")
	assertHeader(t, res, "Vary", "")
}

func TestNoOriginHeader(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"https://allowed.com"},
	})

	res := get(t, url, "")

	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	assertHeader(t, res, "Access-Control-Allow-Origin", "")
}

func TestAllowAllOriginsWithoutCredentials(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"*"},
	})

	res := get(t, url, "https://any.com")

	assertHeader(t, res, "Access-Control-Allow-Origin", "*")
}

func TestAllowAllOriginsWithCredentials(t *testing.T) {
	origin := "https://any.com"
	url := startServer(t, cors.Config{
		AllowedOrigins:   []string{"*"},
		AllowCredentials: true,
	})

	res := get(t, url, origin)

	assertHeader(t, res, "Access-Control-Allow-Origin", origin)
	assertHeader(t, res, "Access-Control-Allow-Credentials", "true")
}

func TestAllowSpecificOrigin(t *testing.T) {
	origin := "https://allowed.com"
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{origin},
	})

	res := get(t, url, origin)

	assertHeader(t, res, "Access-Control-Allow-Origin", origin)
}

func TestVarySetOnlyForAllowedOrigin(t *testing.T) {
	origin := "https://allowed.com"
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{origin},
	})

	resAllowed := get(t, url, origin)
	if resAllowed.Header.Get("Vary") == "" {
		t.Fatal("expected Vary to be set for allowed origin")
	}

	resBlocked := get(t, url, "https://other.com")
	assertHeader(t, resBlocked, "Vary", "")
}

func TestExposeHeadersEmitted(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"*"},
		ExposedHeaders: []string{"X-Request-ID", "X-Total-Count"},
	})

	res := get(t, url, "https://any.com")

	assertHeader(t, res, "Access-Control-Expose-Headers", "X-Request-ID, X-Total-Count")
}

func TestExposeHeadersNotEmittedWhenEmpty(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"*"},
	})

	res := get(t, url, "https://any.com")

	assertHeader(t, res, "Access-Control-Expose-Headers", "")
}

func TestPreflightReturns204(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"https://allowed.com"},
	})

	res := preflight(t, url, "https://allowed.com", "GET", "")

	if res.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", res.StatusCode)
	}
}

func TestPreflightAllowedMethodsDefault(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"*"},
	})

	res := preflight(t, url, "https://any.com", "GET", "")

	assertHeader(t, res, "Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
}

func TestPreflightAllowedMethodsCustom(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST"},
	})

	res := preflight(t, url, "https://any.com", "GET", "")

	assertHeader(t, res, "Access-Control-Allow-Methods", "GET, POST")
}

func TestPreflightMaxAgeEmitted(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"*"},
		MaxAgeSeconds:  3600,
	})

	res := preflight(t, url, "https://any.com", "GET", "")

	assertHeader(t, res, "Access-Control-Max-Age", "3600")
}

func TestPreflightMaxAgeNotEmittedWhenZero(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"*"},
		MaxAgeSeconds:  0,
	})

	res := preflight(t, url, "https://any.com", "GET", "")

	assertHeader(t, res, "Access-Control-Max-Age", "")
}

func TestAllowHeadersWildcardEchosRequestHeaders(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"*"},
		AllowedHeaders: []string{"*"},
	})

	res := preflight(t, url, "https://any.com", "GET", "apikey, authorization")

	assertHeader(t, res, "Access-Control-Allow-Headers", "apikey, authorization")
}

func TestAllowHeadersWildcardWithNoRequestHeaders(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"*"},
		AllowedHeaders: []string{"*"},
	})

	res := preflight(t, url, "https://any.com", "GET", "")

	assertHeader(t, res, "Access-Control-Allow-Headers", "")
}

func TestAllowHeadersExplicitJoined(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"*"},
		AllowedHeaders: []string{"apikey", "authorization"},
	})

	res := preflight(t, url, "https://any.com", "GET", "apikey")

	assertHeader(t, res, "Access-Control-Allow-Headers", "apikey, authorization")
}

func TestAllowHeadersEmptyConfigEchosRequestHeaders(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowedOrigins: []string{"*"},
	})

	res := preflight(t, url, "https://any.com", "GET", "x-custom-header")

	assertHeader(t, res, "Access-Control-Allow-Headers", "x-custom-header")
}