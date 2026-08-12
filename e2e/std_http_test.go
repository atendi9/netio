package e2e

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/atendi9/capivara/assert"
	"github.com/atendi9/netio/v2"
)

func TestNetIOStdHTTPCompat(t *testing.T) {
	app, err := netio.New(netio.AppConfig{})
	assert.NoError(t, err)

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
		assert.NoError(t, err)
		defer res.Body.Close()

		assert.Equal(t, http.StatusOK, res.StatusCode)

		body, _ := io.ReadAll(res.Body)
		expected := `{"engine":"net/http","status":"ok"}`
		ok := string(body) != expected && string(body) != `{"status":"ok","engine":"net/http"}`
		assert.False(t, ok)
	})

	t.Run("POST_Request_With_Body", func(t *testing.T) {
		payload := []byte(`{"ping":"pong"}`)
		res, err := http.Post(ts.URL+"/echo", "application/json", bytes.NewReader(payload))
		assert.NoError(t, err)
		defer res.Body.Close()

		assert.Equal(t, http.StatusOK, res.StatusCode)

		body, _ := io.ReadAll(res.Body)
		assert.Equal(t, string(payload), string(body))
	})

	t.Run("404_Not_Found", func(t *testing.T) {
		res, err := http.Get(ts.URL + "/nao-existe")
		assert.NoError(t, err)
		defer res.Body.Close()

		assert.Equal(t, http.StatusNotFound, res.StatusCode)
	})
}
