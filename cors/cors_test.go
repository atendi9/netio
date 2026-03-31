package cors_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/atendi9/netio"
	"github.com/atendi9/netio/cors" 
)

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

	if res.StatusCode != http.StatusOK {
		t.Errorf("Esperava status %d, recebeu %d", http.StatusOK, res.StatusCode)
	}

	allowedOrigin := res.Header.Get("Access-Control-Allow-Origin")
	if allowedOrigin != "https://meusite.com.br" {
		t.Errorf("Esperava Access-Control-Allow-Origin 'https://meusite.com.br', recebeu '%s'", allowedOrigin)
	}
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

	if res.StatusCode != http.StatusNoContent {
		t.Errorf("Esperava status %d para Preflight, recebeu %d", http.StatusNoContent, res.StatusCode)
	}

	headers := map[string]string{
		"Access-Control-Allow-Origin":  "https://meusite.com.br",
		"Access-Control-Allow-Methods": "POST, GET, OPTIONS",
		"Access-Control-Allow-Headers": "Authorization, Content-Type",
		"Access-Control-Max-Age":       "86400",
	}

	for key, expected := range headers {
		actual := res.Header.Get(key)
		if actual != expected {
			t.Errorf("Header %s incorreto. Esperava '%s', recebeu '%s'", key, expected, actual)
		}
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
	if allowedOrigin != "" {
		t.Errorf("A origem não deveria ser permitida, mas o header retornou: '%s'", allowedOrigin)
	}
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
	if allowedOrigin != "*" {
		t.Errorf("Esperava Access-Control-Allow-Origin '*', recebeu '%s'", allowedOrigin)
	}
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