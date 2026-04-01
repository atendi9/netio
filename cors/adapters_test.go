package cors

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/atendi9/handlerx"
	"github.com/atendi9/netio"
)

func TestCORSWithNetIOAdapter(t *testing.T) {
	app := setupApp()
	app.Use(app.Cors("https://meusite.com.br"))
	handlerConverter := NetIOConverter(app)
	app.Get("/api/data", handlerConverter.Convert(func(c handlerx.Context) handlerx.Response {
		return handlerx.Response{Data: map[string]any{"message": "Hello, World!"}}
	}))

	t.Cleanup(func() {
		if err := app.Shutdown(); err != nil {
			t.Errorf("shutdown error: %v", err)
		}
	})

	go func() {
		app.Listen()
	}()
	time.Sleep(1 *time.Second)
	res, err := http.Get("http://localhost:5090/api/data")
	if err != nil {
		t.Fatalf("Erro ao fazer requisição: %v", err)
	}

	if res.StatusCode != http.StatusOK {
		t.Errorf("Esperava status %d, recebeu %d", http.StatusOK, res.StatusCode)
	}
	t.Logf("Headers: %v", res.Header)
	allowedOrigin := res.Header.Get("Access-Control-Allow-Origin")
	if allowedOrigin != "https://meusite.com.br" {
		t.Errorf("Esperava Access-Control-Allow-Origin 'https://meusite.com.br', recebeu '%s'", allowedOrigin)
	}
}

func setupApp() NetIORouter {
	app := NetIOAdapter("5090")
	return app
}

type netIOConverter struct {
	Converter[netio.Handler]
}

func NetIOConverter(app NetIORouter) netIOConverter {
	return netIOConverter{Converter[netio.Handler]{app}}
}

type Converter[H any] struct {
	app any
}

func (hc Converter[H]) Convert(h handlerx.Handler) H {
	return any(NetIO(h)).(H)
}

func NetIO(h handlerx.Handler) netio.Handler {
	return func(c *netio.Context) {
		res := h(c)
		if res.GoNext() {
			return
		}
		if len(res.FilePath) > 0 {
			c.SendFile(res.FilePath)
			return
		}
		if err := res.Err; err != nil {
			c.Status(res.Status()).JSON(map[string]any{
				"err": err.Error(),
			})
			return
		}
		if v, ok := res.Data.(string); ok {
			c.Status(res.Status()).Send([]byte(v))
			return
		}
		c.Status(res.Status()).JSON(res.Data)
	}
}

type NetIORouter interface {
	Use(middlewares ...netio.Handler)
	Cors(allowedOrigins ...string) netio.Handler
	netio.Router
	Listen() error
	Shutdown() error
}

type netioAdapter struct {
	s   *netio.App
}

func NetIOAdapter(port string) NetIORouter {
	adapter := &netioAdapter{}
	appName := "Atendi9"
	adapter.s, _ = netio.New(netio.AppConfig{
		Port:        port,
		AppName:     appName,
		MaxBodySize: "30 MB",
	})
	return adapter
}

func (r *netioAdapter) Cors(allowedOrigins ...string) netio.Handler {
	minutes := 60
	hour := minutes * minutes
	return Middleware(Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		MaxAge:           1 * hour,
		AllowCredentials: true,
	})
}

func (r *netioAdapter) Use(middlewares ...netio.Handler) {
	for _, middleware := range middlewares {
		r.s.Use(middleware)
	}
}

func (r *netioAdapter) Get(endpoint string, handlers ...netio.Handler) {
	r.s.GET(endpoint, handlers...)
}

func (r *netioAdapter) Post(endpoint string, handlers ...netio.Handler) {
	r.s.POST(endpoint, handlers...)
}

func (r *netioAdapter) Put(endpoint string, handlers ...netio.Handler) {
	r.s.PUT(endpoint, handlers...)
}

func (r *netioAdapter) Patch(endpoint string, handlers ...netio.Handler) {
	r.s.PATCH(endpoint, handlers...)
}

func (r *netioAdapter) Delete(endpoint string, handlers ...netio.Handler) {
	r.s.DELETE(endpoint, handlers...)
}

func (r *netioAdapter) Group(basePath string, middlewares ...netio.Handler) netio.Router {
	return r.s.Group(basePath, middlewares...)
}

func (r *netioAdapter) Route(basePath string, fn func(netio.Router)) {
	group := r.s.Group(basePath)
	fn(group)
}

func (r *netioAdapter) Listen() error {
	return r.s.Listen()
}

func (r *netioAdapter) Shutdown() error {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return r.s.Shutdown(ctx)
}
