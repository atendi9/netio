package netio

import (
	"net/http/httptest"
	"testing"
)

// ServeHTTP never populated ctx.query from the URL, so Context.Query read empty
// for every method when the app was mounted as an http.Handler, while the same
// route served over a raw socket resolved it fine.
func TestQueryString_PopulatedViaServeHTTP(t *testing.T) {
	app, _ := New(AppConfig{})

	var limit, cursor string
	app.GET("/items", func(c *Context) {
		limit = c.Query("limit")
		cursor = c.Query("cursor", "first")
		c.SendStatus(200)
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/items?limit=10", nil))
	rec.Result().Body.Close()

	if limit != "10" {
		t.Errorf("c.Query(%q) = %q, expected %q", "limit", limit, "10")
	}
	if cursor != "first" {
		t.Errorf("c.Query with a default = %q, expected the default", cursor)
	}
}

func TestQueryString_MultipleParamsViaServeHTTP(t *testing.T) {
	app, _ := New(AppConfig{})

	var got map[string]string
	app.GET("/items", func(c *Context) {
		got = map[string]string{
			"limit":  c.Query("limit"),
			"cursor": c.Query("cursor"),
			"flag":   c.Query("flag"),
		}
		c.SendStatus(200)
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/items?limit=10&cursor=abc&flag", nil))
	rec.Result().Body.Close()

	if got["limit"] != "10" || got["cursor"] != "abc" {
		t.Errorf("query params = %v, expected limit=10 cursor=abc", got)
	}
	// A bare key with no "=" is present with an empty value, matching the
	// raw-socket parser.
	if _, ok := got["flag"]; !ok {
		t.Error("valueless query key was dropped")
	}
}

// No query string must not synthesize entries.
func TestQueryString_AbsentViaServeHTTP(t *testing.T) {
	app, _ := New(AppConfig{})

	var got string
	app.GET("/items", func(c *Context) {
		got = c.Query("limit", "fallback")
		c.SendStatus(200)
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", "/items", nil))
	rec.Result().Body.Close()

	if got != "fallback" {
		t.Errorf("c.Query = %q, expected the default %q", got, "fallback")
	}
}
