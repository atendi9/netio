package e2e

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/atendi9/netio"
)

func TestNetIOStdHTTPCompat(t *testing.T) {
	app, err := netio.New(netio.AppConfig{})
	if err != nil {
		t.Fatalf("failed to create app: %v", err)
	}

	app.GET("/hello", func(c *netio.Context) {
		c.JSON(map[string]string{"status": "ok", "engine": "net/http"})
	})

	app.POST("/echo", func(c *netio.Context) {
		body := c.Body()
		c.Send(body)
	})

	ts := httptest.NewServer(app)
	defer ts.Close()

	t.Run("GET_Request", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/hello")
		if err != nil {
			t.Fatalf("GET request failed: %v", err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", res.StatusCode)
		}

		body, _ := io.ReadAll(res.Body)
		expected := `{"engine":"net/http","status":"ok"}`

		if string(body) != expected && string(body) != `{"status":"ok","engine":"net/http"}` {
			t.Errorf("unexpected body: %s", body)
		}
	})

	t.Run("POST_Request_With_Body", func(t *testing.T) {
		payload := []byte(`{"ping":"pong"}`)
		res, err := http.Post(ts.URL+"/echo", "application/json", bytes.NewReader(payload))
		if err != nil {
			t.Fatalf("POST request failed: %v", err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusOK {
			t.Errorf("expected status 200, got %d", res.StatusCode)
		}

		body, _ := io.ReadAll(res.Body)
		if string(body) != string(payload) {
			t.Errorf("expected body %q, got %q", string(payload), string(body))
		}
	})

	t.Run("404_Not_Found", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/nao-existe")
		if err != nil {
			t.Fatalf("GET request failed: %v", err)
		}
		defer res.Body.Close()

		if res.StatusCode != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", res.StatusCode)
		}
	})
}
