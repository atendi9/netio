package netio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// queryRequest builds a QUERY request whose content carries the query, which is
// what distinguishes the method from GET.
func queryRequest(path, contentType, body string) *http.Request {
	req := httptest.NewRequest("QUERY", path, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}

func TestQuery_RoutesAndReadsContent(t *testing.T) {
	app, err := New(AppConfig{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var got string
	app.QUERY("/search", func(c *Context) {
		got = string(c.Body())
		c.JSON(map[string]any{"matched": 2})
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, queryRequest("/search", "application/sql", "SELECT * FROM t"))

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	if got != "SELECT * FROM t" {
		t.Errorf("handler saw body %q, expected the query content", got)
	}
}

// RFC 10008 §2: "Servers MUST fail the request if the Content-Type request field
// is missing or is inconsistent with the request content."
func TestQuery_RejectsMissingContentType(t *testing.T) {
	app, _ := New(AppConfig{})

	reached := false
	app.QUERY("/search", func(c *Context) {
		reached = true
		c.SendStatus(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, queryRequest("/search", "", "SELECT 1"))

	res := rec.Result()
	defer res.Body.Close()

	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for a QUERY with no Content-Type, got %d", res.StatusCode)
	}
	if reached {
		t.Error("handler ran despite the missing Content-Type")
	}
}

// The guard must not swallow a request that does declare its media type.
func TestQuery_AllowsAnyPresentContentType(t *testing.T) {
	for _, contentType := range []string{
		"application/sql",
		"application/json",
		"application/graphql",
		"text/plain; charset=utf-8",
	} {
		t.Run(contentType, func(t *testing.T) {
			app, _ := New(AppConfig{})
			app.QUERY("/search", func(c *Context) { c.SendStatus(http.StatusOK) })

			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, queryRequest("/search", contentType, "q"))

			res := rec.Result()
			defer res.Body.Close()
			if res.StatusCode != http.StatusOK {
				t.Errorf("expected 200 for Content-Type %q, got %d", contentType, res.StatusCode)
			}
		})
	}
}

// QUERY is safe and idempotent, so repeating it must produce the same result and
// leave no accumulated state behind.
func TestQuery_IsIdempotent(t *testing.T) {
	app, _ := New(AppConfig{})
	app.QUERY("/search", func(c *Context) {
		c.JSON(map[string]any{"echo": string(c.Body())})
	})

	var bodies []string
	for range 3 {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, queryRequest("/search", "application/sql", "SELECT 1"))

		res := rec.Result()
		var payload map[string]any
		if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
			t.Fatalf("decode: %v", err)
		}
		res.Body.Close()
		bodies = append(bodies, payload["echo"].(string))
	}

	for i, b := range bodies {
		if b != "SELECT 1" {
			t.Errorf("repetition %d returned %q, expected an identical result", i, b)
		}
	}
}

// Path params and the query string still work, so a QUERY can scope its content
// to a resource the way the other methods do.
func TestQuery_SupportsParamsAndQueryString(t *testing.T) {
	app, _ := New(AppConfig{})

	var gotParam, gotQuery string
	app.QUERY("/tenants/:id/search", func(c *Context) {
		gotParam = c.Params("id")
		gotQuery = c.Query("limit")
		c.SendStatus(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, queryRequest("/tenants/42/search?limit=10", "application/sql", "SELECT 1"))
	rec.Result().Body.Close()

	if gotParam != "42" {
		t.Errorf("param id = %q, expected \"42\"", gotParam)
	}
	if gotQuery != "10" {
		t.Errorf("query limit = %q, expected \"10\"", gotQuery)
	}
}

// Registering QUERY must not make the path answer other methods.
func TestQuery_DoesNotAnswerOtherMethods(t *testing.T) {
	app, _ := New(AppConfig{})
	app.QUERY("/search", func(c *Context) { c.SendStatus(http.StatusOK) })

	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/search", strings.NewReader(""))
			req.Header.Set("Content-Type", "application/sql")
			app.ServeHTTP(rec, req)

			res := rec.Result()
			defer res.Body.Close()
			if res.StatusCode != http.StatusNotFound {
				t.Errorf("%s /search returned %d, expected 404", method, res.StatusCode)
			}
		})
	}
}

// QUERY registers the OPTIONS route like the other methods, so a preflight for
// it reaches the middleware chain instead of 404ing.
func TestQuery_RegistersOptions(t *testing.T) {
	app, _ := New(AppConfig{})
	app.QUERY("/search", func(c *Context) { c.SendStatus(http.StatusOK) })

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/search", nil))

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Errorf("OPTIONS /search returned %d, expected 204", res.StatusCode)
	}
}

func TestQuery_GroupRegistersRoute(t *testing.T) {
	app, _ := New(AppConfig{})

	var middlewareRan bool
	g := app.Group("/v1", func(c *Context) { middlewareRan = true })
	g.Query("/search", func(c *Context) { c.SendStatus(http.StatusOK) })

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, queryRequest("/v1/search", "application/sql", "SELECT 1"))

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.StatusCode)
	}
	if !middlewareRan {
		t.Error("group middleware did not run for the QUERY route")
	}
}

// A QUERY rejected for a missing Content-Type must still carry the headers set
// by global middleware. This is the CORS case: a 400 with no Access-Control-*
// headers reaches the browser as an opaque CORS failure rather than as the 400
// it is. Uses a plain middleware because importing the cors package from here
// would be an import cycle; the mechanism exercised is identical.
func TestQuery_GlobalMiddlewareHeadersSurviveRejection(t *testing.T) {
	app, _ := New(AppConfig{})
	app.Use(func(c *Context) { c.HeaderSet("Access-Control-Allow-Origin", "https://app.com") })
	app.QUERY("/search", func(c *Context) { c.SendStatus(http.StatusOK) })

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, queryRequest("/search", "", "SELECT 1"))

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "https://app.com" {
		t.Errorf("400 lost the middleware header, got %q", got)
	}
}

// The group's Content-Type guard must sit ahead of the route handler but behind
// the group middleware, so a rejected QUERY still passes through middleware that
// sets response headers (CORS being the case that matters).
func TestQuery_GroupMiddlewareRunsBeforeContentTypeGuard(t *testing.T) {
	app, _ := New(AppConfig{})

	g := app.Group("/v1", func(c *Context) { c.HeaderSet("X-Trace", "on") })
	g.Query("/search", func(c *Context) { c.SendStatus(http.StatusOK) })

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, queryRequest("/v1/search", "", "SELECT 1"))

	res := rec.Result()
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
	if got := res.Header.Get("X-Trace"); got != "on" {
		t.Errorf("middleware header lost on the 400 response, got %q", got)
	}
}
