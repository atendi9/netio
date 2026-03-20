package netio

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
)

// App represents a netio HTTP application.
type App struct {
	appName     string
	port        string
	root        *node
	mw          []Handler
	maxBodySize int
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
	if len(config.AppName) > 0 {
		app.appName = config.AppName
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
	var isFirstStartup = true
	for {
		if isFirstStartup {
			a.startup()
			isFirstStartup = false
		}
		conn, _ := ln.Accept()
		go a.serve(conn)
	}
}

func (a *App) startup() {
	a.log(
		a.newMsg("http.server is running"),
		a.newMsg(fmt.Sprintf("http://localhost:%s\n", a.port)),
	)
}

func (a *App) log(msgs ...string) {
	message := strings.Join(msgs, "")
	fmt.Print(message)
}

func (a *App) newMsg(msg string) string {
	return fmt.Sprintf("\r%s ▷ %s\n", a.appName, msg)
}

func (a *App) serve(conn net.Conn) {
	defer conn.Close()

	r := bufio.NewReader(conn)

	for {
		ctx := ctxPool.Get().(*Context)
		ctx.reset()
		ctx.conn = conn
		ctx.maxBodySize = a.maxBodySize

		if !parseRequest(r, ctx) {
			ctxPool.Put(ctx)
			return
		}

		if !checkBodySize(ctx) {
			ctxPool.Put(ctx)
			return
		}

		params := make([]KV, 0, 8)
		h, ok := a.root.findMethod(string(ctx.method), splitBytes(ctx.path), &params)
		if !ok {
			writeResponseWithHeaders(conn, 404, []byte("Not Found"), ctx.resHeader)
			ctxPool.Put(ctx)
			return
		}

		ctx.params = params
		ctx.handlers = append(a.mw, h...)
		ctx.Next()

		if !keepAlive(ctx) {
			ctxPool.Put(ctx)
			return
		}

		ctxPool.Put(ctx)
	}
}

func checkBodySize(ctx *Context) bool {
	if ctx.maxBodySize <= 0 {
		return true
	}

	cl := header(ctx, []byte("Content-Length"))
	if cl == nil {
		return true
	}

	size, err := strconv.Atoi(string(cl))
	if err != nil {
		writeResponseWithHeaders(ctx.conn, 400, []byte("Bad Request"), ctx.resHeader)
		return false
	}

	if size > ctx.maxBodySize {
		writeResponseWithHeaders(ctx.conn, 413, []byte("Payload Too Large"), ctx.resHeader)
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
