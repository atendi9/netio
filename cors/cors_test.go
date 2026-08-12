package cors_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/atendi9/capivara/assert"
	"github.com/atendi9/netio/v2"
	"github.com/atendi9/netio/v2/cors"
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
		AllowOrigins: []string{"https://allowed.com"},
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
		AllowOrigins: []string{"https://allowed.com"},
	})

	res := get(t, url, "")

	if res.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	assertHeader(t, res, "Access-Control-Allow-Origin", "")
}

func TestAllowAllOriginsWithoutCredentials(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"*"},
	})

	res := get(t, url, "https://any.com")

	assertHeader(t, res, "Access-Control-Allow-Origin", "*")
}

func TestAllowAllOriginsWithCredentials(t *testing.T) {
	origin := "https://any.com"
	url := startServer(t, cors.Config{
		AllowOrigins:     []string{"*"},
		AllowCredentials: true,
	})

	res := get(t, url, origin)

	assertHeader(t, res, "Access-Control-Allow-Origin", origin)
	assertHeader(t, res, "Access-Control-Allow-Credentials", "true")
}

func TestAllowSpecificOrigin(t *testing.T) {
	origin := "https://allowed.com"
	url := startServer(t, cors.Config{
		AllowOrigins: []string{origin},
	})

	res := get(t, url, origin)

	assertHeader(t, res, "Access-Control-Allow-Origin", origin)
}

func TestVarySetOnlyForAllowedOrigin(t *testing.T) {
	origin := "https://allowed.com"
	url := startServer(t, cors.Config{
		AllowOrigins: []string{origin},
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
		AllowOrigins:  []string{"*"},
		ExposeHeaders: []string{"X-Request-ID", "X-Total-Count"},
	})

	res := get(t, url, "https://any.com")

	assertHeader(t, res, "Access-Control-Expose-Headers", "X-Request-ID, X-Total-Count")
}

func TestExposeHeadersNotEmittedWhenEmpty(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"*"},
	})

	res := get(t, url, "https://any.com")

	assertHeader(t, res, "Access-Control-Expose-Headers", "")
}

func TestPreflightReturns204(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"https://allowed.com"},
	})

	res := preflight(t, url, "https://allowed.com", "GET", "")

	if res.StatusCode != 204 {
		t.Fatalf("expected 204, got %d", res.StatusCode)
	}
}

func TestPreflightAllowedMethodsDefault(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"*"},
	})

	res := preflight(t, url, "https://any.com", "GET", "")

	assertHeader(t, res, "Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS, QUERY")
}

func TestPreflightAllowedMethodsCustom(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{"GET", "POST"},
	})

	res := preflight(t, url, "https://any.com", "GET", "")

	assertHeader(t, res, "Access-Control-Allow-Methods", "GET, POST")
}

func TestPreflightMaxAgeEmitted(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"*"},
		MaxAge:       3600,
	})

	res := preflight(t, url, "https://any.com", "GET", "")

	assertHeader(t, res, "Access-Control-Max-Age", "3600")
}

func TestPreflightMaxAgeNotEmittedWhenZero(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"*"},
		MaxAge:       0,
	})

	res := preflight(t, url, "https://any.com", "GET", "")

	assertHeader(t, res, "Access-Control-Max-Age", "")
}

func TestAllowHeadersWildcardEchosRequestHeaders(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{"*"},
	})

	res := preflight(t, url, "https://any.com", "GET", "apikey, authorization")

	assertHeader(t, res, "Access-Control-Allow-Headers", "apikey, authorization")
}

func TestAllowHeadersWildcardWithNoRequestHeaders(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{"*"},
	})

	res := preflight(t, url, "https://any.com", "GET", "")

	assertHeader(t, res, "Access-Control-Allow-Headers", "")
}

func TestAllowHeadersExplicitJoined(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"*"},
		AllowHeaders: []string{"apikey", "authorization"},
	})

	res := preflight(t, url, "https://any.com", "GET", "apikey")

	assertHeader(t, res, "Access-Control-Allow-Headers", "apikey, authorization")
}

func TestAllowHeadersEmptyConfigEchosRequestHeaders(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"*"},
	})

	res := preflight(t, url, "https://any.com", "GET", "x-custom-header")

	assertHeader(t, res, "Access-Control-Allow-Headers", "x-custom-header")
}

func TestCORS_AllowedOrigin_SimpleRequest(t *testing.T) {
	app := setupApp(cors.Config{
		AllowOrigins: []string{"https://meusite.com.br"},
		AllowMethods: []string{"GET"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Origin", "https://meusite.com.br")

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	res := rec.Result()
	assert.Equal(t, http.StatusOK, res.StatusCode)

	allowedOrigin := res.Header.Get("Access-Control-Allow-Origin")
	assert.Equal(t, "https://meusite.com.br", allowedOrigin)
}

func TestCORS_PreflightRequest(t *testing.T) {
	app := setupApp(cors.Config{
		AllowOrigins: []string{"https://meusite.com.br"},
		AllowMethods: []string{"POST", "GET", "OPTIONS"},
		AllowHeaders: []string{"Authorization", "Content-Type"},
		MaxAge:       86400,
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/data", nil)
	req.Header.Set("Origin", "https://meusite.com.br")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Authorization")

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	res := rec.Result()
	assert.Equal(t, http.StatusNoContent, res.StatusCode)

	headers := map[string]string{
		"Access-Control-Allow-Origin":  "https://meusite.com.br",
		"Access-Control-Allow-Methods": "POST, GET, OPTIONS",
		"Access-Control-Allow-Headers": "Authorization, Content-Type",
		"Access-Control-Max-Age":       "86400",
	}

	for key, expected := range headers {
		actual := res.Header.Get(key)
		assert.Equal(t, expected, actual)
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	app := setupApp(cors.Config{
		AllowOrigins: []string{"https://meusite.com.br"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Origin", "https://site-malicioso.com")

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	res := rec.Result()

	allowedOrigin := res.Header.Get("Access-Control-Allow-Origin")
	assert.Empty(t, allowedOrigin)
}

func TestCORS_AllowAllOrigins(t *testing.T) {
	app := setupApp(cors.Config{
		AllowOrigins: []string{"*"},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/data", nil)
	req.Header.Set("Origin", "https://qualqueresite.com")

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	res := rec.Result()

	allowedOrigin := res.Header.Get("Access-Control-Allow-Origin")
	assert.Equal(t, "*", allowedOrigin)
}

// A preflight for a path with no registered route must still run the middleware
// chain: answering it inline would skip CORS and leave the browser with a bare
// 204 carrying no headers.
func TestPreflightOnUnregisteredRouteRunsMiddleware(t *testing.T) {
	app := setupApp(cors.Config{
		AllowOrigins: []string{"https://app.com"},
		AllowMethods: []string{"GET", "POST"},
	})

	req := httptest.NewRequest(http.MethodOptions, "/no/such/route", nil)
	req.Header.Set("Origin", "https://app.com")
	req.Header.Set("Access-Control-Request-Method", "GET")

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.StatusCode)
	}
	assert.Equal(t, "https://app.com", res.Header.Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "GET, POST", res.Header.Get("Access-Control-Allow-Methods"))
}

// Config values commonly arrive from a single environment variable, so the
// origin list must survive comma-joining, stray whitespace, a trailing slash
// and mixed case rather than silently matching nothing.
func TestOriginNormalization(t *testing.T) {
	cases := []struct {
		name         string
		allowOrigins []string
		origin       string
	}{
		{"trailing slash in config", []string{"https://app.com/"}, "https://app.com"},
		{"trailing slash in request", []string{"https://app.com"}, "https://app.com/"},
		{"comma-joined single entry", []string{"https://a.com,https://b.com"}, "https://b.com"},
		{"whitespace around entries", []string{" https://a.com , https://b.com "}, "https://a.com"},
		{"uppercase host in config", []string{"https://APP.com"}, "https://app.com"},
		{"uppercase scheme in request", []string{"https://app.com"}, "HTTPS://app.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := startServer(t, cors.Config{
				AllowOrigins:     tc.allowOrigins,
				AllowMethods:     []string{"GET,POST", " OPTIONS "},
				AllowCredentials: true,
			})

			res := preflight(t, url, tc.origin, "GET", "authorization")

			if res.StatusCode != http.StatusNoContent {
				t.Fatalf("expected 204, got %d", res.StatusCode)
			}
			// The raw request origin is echoed back, not the normalized form.
			assertHeader(t, res, "Access-Control-Allow-Origin", tc.origin)
			assertHeader(t, res, "Access-Control-Allow-Credentials", "true")
			assertHeader(t, res, "Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		})
	}
}

// A genuinely foreign origin must still be rejected after normalization.
func TestOriginNormalizationDoesNotOverMatch(t *testing.T) {
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"https://app.com"},
	})

	for _, origin := range []string{
		"https://app.com.evil.com",
		"http://app.com",
		"https://app.com:8443",
		"https://notapp.com",
	} {
		res := get(t, url, origin)
		assertHeader(t, res, "Access-Control-Allow-Origin", "")
	}
}

// AllowOriginFunc receives the origin exactly as the browser sent it.
func TestAllowOriginFuncReceivesRawOrigin(t *testing.T) {
	var seen string
	url := startServer(t, cors.Config{
		AllowOrigins: []string{"https://other.com"},
		AllowOriginFunc: func(origin string) bool {
			seen = origin
			return true
		},
	})

	const raw = "HTTPS://Sub.App.com/"
	res := get(t, url, raw)

	if seen != raw {
		t.Fatalf("AllowOriginFunc got %q, expected raw %q", seen, raw)
	}
	assertHeader(t, res, "Access-Control-Allow-Origin", raw)
}

func setupApp(config cors.Config) *netio.App {
	app, _ := netio.New(netio.AppConfig{})
	app.Use(cors.Middleware(config))
	app.GET("/api/data", func(c *netio.Context) {
		c.SendStatus(http.StatusOK)
		c.Send([]byte("sucesso"))
	})

	return app
}

// DefaultConfig has to advertise every method the router registers, QUERY
// included, or a preflight for one of them is rejected out of the box.
func TestDefaultConfig(t *testing.T) {
	cfg := cors.DefaultConfig()

	if len(cfg.AllowOrigins) != 1 || cfg.AllowOrigins[0] != cors.AllowAll {
		t.Errorf("AllowOrigins = %v, want [%s]", cfg.AllowOrigins, cors.AllowAll)
	}
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "QUERY"} {
		if !slices.Contains(cfg.AllowMethods, method) {
			t.Errorf("AllowMethods = %v, missing %s", cfg.AllowMethods, method)
		}
	}
}
