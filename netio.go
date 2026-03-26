package netio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

type startFn func(port string)

// App represents a netio HTTP application.
type App struct {
	appName     string
	port        string
	startFn     startFn
	logger      Logger
	root        *node
	mw          []Handler
	maxBodySize int
	ln          net.Listener
}

// MaxBodySize is a string type for configuration of max body size.
type MaxBodySize string

// String returns the max body size as a string, with default "15 MB".
func (s MaxBodySize) String() string {
	if len(s) == 0 {
		return "15 MB"
	}
	return string(s)
}

// AppConfig represents configuration options for a new App.
type AppConfig struct {
	Port        string
	AppName     string
	MaxBodySize MaxBodySize
	Logger      Logger
	Startup     startFn
}

const defaultAppName = "netio"

// New creates a new App instance based on AppConfig.
func New(config AppConfig) (*App, error) {
	maxBodySize, err := generateMaxBodySize(config.MaxBodySize)
	if err != nil {
		return nil, err
	}
	app := &App{
		appName:     defaultAppName,
		port:        config.Port,
		root:        &node{},
		maxBodySize: maxBodySize,
	}
	if config.Startup != nil {
		app.startFn = config.Startup
	}
	if len(app.port) == 0 {
		listener, err := net.Listen("tcp", ":0")
		if err != nil {
			return nil, err
		}
		defer listener.Close()

		addr := listener.Addr().String()
		_, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		app.port = port
	}
	if len(config.AppName) > 0 {
		app.appName = config.AppName
	}
	if config.Logger != nil {
		app.logger = config.Logger
	} else {
		app.logger = NewDefaultLogger(app.appName)
	}
	return app, nil
}

var (
	ErrInvalidSize              = errors.New("invalid maxBodySize")
	ErrUnknowUnit               = errors.New("unknown unit")
	ErrInvalidMaxBodySizeFormat = errors.New("invalid format")
)

func generateMaxBodySize(mbs MaxBodySize) (int, error) {
	s := mbs.String()
	s = strings.TrimSpace(strings.ToUpper(s))
	s = strings.ReplaceAll(s, " ", "")

	if len(s) < 2 {
		return 0, ErrInvalidSize
	}
	var numPart string
	var unitPart string

	for i, r := range s {
		if r < '0' || r > '9' {
			numPart = s[:i]
			unitPart = s[i:]
			break
		}
	}

	if numPart == "" || unitPart == "" {
		return 0, ErrInvalidMaxBodySizeFormat
	}

	value := atoi([]byte(numPart))

	switch unitPart {
	case "B":
		return value, nil
	case "KB":
		return value << 10, nil
	case "MB":
		return value << 20, nil
	case "GB":
		return value << 30, nil
	case "TB":
		return value << 40, nil
	default:
		return 0, ErrUnknowUnit
	}
}

// Use adds a global middleware handler.
func (a *App) Use(h Handler) {
	a.mw = append(a.mw, h)
}

// GET registers a GET route with handlers.
func (a *App) GET(path string, h ...Handler) {
	a.root.addMethod("GET", split(path), h)
}

// POST registers a POST route with handlers.
func (a *App) POST(path string, h ...Handler) {
	a.root.addMethod("POST", split(path), h)
}

// PUT registers a PUT route with handlers.
func (a *App) PUT(path string, h ...Handler) {
	a.root.addMethod("PUT", split(path), h)
}

// DELETE registers a DELETE route with handlers.
func (a *App) DELETE(path string, h ...Handler) {
	a.root.addMethod("DELETE", split(path), h)
}

// PATCH registers a PATCH route with handlers.
func (a *App) PATCH(path string, h ...Handler) {
	a.root.addMethod("PATCH", split(path), h)
}

// Listen starts the HTTP server on the configured port.
func (a *App) Listen() error {
	ln, err := net.Listen("tcp", ":"+a.port)
	if err != nil {
		return err
	}

	a.ln = ln

	var isFirstStartup = true
	for {
		if isFirstStartup {
			a.startup()
			isFirstStartup = false
		}

		conn, err := ln.Accept()
		if err != nil {
			return err
		}

		go a.serve(conn)
	}
}

// Shutdown gracefully stops the application listener when the context is done.
//
// It blocks until the context is canceled or reaches its deadline,
// then closes the underlying network listener, causing any blocking
// Accept calls to return.
//
// If the listener is not initialized, Shutdown returns nil.
func (a *App) Shutdown(ctx context.Context) error {
	if a.ln == nil {
		return nil
	}

	<-ctx.Done()
	return a.ln.Close()
}

func (a *App) startup() {
	if a.startFn != nil {
		a.startFn(a.port)
		return
	}
	a.log(
		"http.server is running\n",
		fmt.Sprintf("http://localhost:%s\n", a.port),
	)
}

func (a *App) serve(conn net.Conn) {
	defer conn.Close()

	r := bufio.NewReader(conn)

	for {
		ctx := ctxPool.Get().(*Context)
		ctx.reset()
		ctx.appName = a.appName
		ctx.conn = conn
		ctx.maxBodySize = a.maxBodySize

		if !parseRequest(r, ctx) {
			ctxPool.Put(ctx)
			return
		}

		if !a.checkBodySize(ctx) {
			ctxPool.Put(ctx)
			return
		}

		params := make([]KV, 0, 8)

		h, ok := a.root.findMethod(string(ctx.method), splitBytes(ctx.path), &params)
		if ok {
			ctx.params = params
			ctx.handlers = append([]Handler{}, a.mw...)
			ctx.handlers = append(ctx.handlers, h...)
		} else if ctx.Method() != "OPTIONS" {
			ctx.params = params
			ctx.handlers = append([]Handler{}, a.mw...)
			ctx.handlers = append(ctx.handlers, func(c *Context) {
				c.SendStatus(http.StatusNotFound)
			})
		} else {
			ctx.params = params
			for _, h := range append(ctx.handlers, a.mw...) {
				h(ctx)
				if ctx.wrote {
					break
				}
			}
			if !ctx.wrote {
				ctx.SendStatus(http.StatusNoContent)
				continue
			}
			continue
		}

		ctx.index = -1

		ctx.Next()
		if ctx.wrote {
			if !keepAlive(ctx) {
				ctxPool.Put(ctx)
				return
			}
			ctxPool.Put(ctx)
			continue
		}

		if !ctx.wrote {
			ctx.SendStatus(http.StatusNoContent)
		}

		if !keepAlive(ctx) {
			ctxPool.Put(ctx)
			return
		}

		ctxPool.Put(ctx)
	}
}

func (a *App) checkBodySize(ctx *Context) bool {
	if ctx.maxBodySize <= 0 {
		return true
	}

	cl := header(ctx, []byte("Content-Length"))
	if cl == nil {
		return true
	}

	size, err := strconv.Atoi(string(cl))
	if err != nil {
		ctx.SendStatus(400)
		return false
	}

	if size > ctx.maxBodySize {
		ctx.SendStatus(413)
		return false
	}

	return true
}

func header(c *Context, k []byte) []byte {
	for i := range c.header {
		if bytes.Equal(c.header[i].K, k) {
			return c.header[i].V
		}
	}
	return nil
}

func detectContentType(body []byte) string {
	if json.Valid(body) {
		return "application/json"
	}
	return http.DetectContentType(body)
}
