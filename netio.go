package netio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Handler defines the signature for request handler functions
// that process a Context.
type Handler func(c *Context)

type startFn func(port string)

const defaultReadTimeout = 60 * time.Second

// defaultMaxConns bounds the number of connections served concurrently.
// Without a cap, a slowloris-style client opening many keep-alive connections
// (each held open for defaultReadTimeout) would exhaust goroutines and file
// descriptors.
const defaultMaxConns = 1024

type App struct {
	appName            string
	port               string
	startFn            startFn
	logger             Logger
	root               *node
	mw                 []Handler
	maxBodySize        int
	readTimeout        time.Duration
	ln                 net.Listener
	activeConns        sync.WaitGroup
	mu                 sync.Mutex
	conns              map[net.Conn]struct{}
	connSem            chan struct{}
	isFirstStartupHTTP atomic.Bool
}

type MaxBodySize string

func (s MaxBodySize) String() string {
	if len(s) == 0 {
		return "15 MB"
	}
	return string(s)
}

type AppConfig struct {
	Port        string
	AppName     string
	MaxBodySize MaxBodySize
	Logger      Logger
	Startup     startFn
	// MaxConns caps the number of connections served concurrently.
	// A non-positive value falls back to defaultMaxConns.
	MaxConns int
}

const defaultAppName = "netio"

func New(config AppConfig) (*App, error) {
	maxBodySize, err := generateMaxBodySize(config.MaxBodySize)
	if err != nil {
		return nil, err
	}

	maxConns := config.MaxConns
	if maxConns <= 0 {
		maxConns = defaultMaxConns
	}

	app := &App{
		appName:     defaultAppName,
		port:        config.Port,
		root:        &node{},
		maxBodySize: maxBodySize,
		readTimeout: defaultReadTimeout,
		conns:       make(map[net.Conn]struct{}),
		connSem:     make(chan struct{}, maxConns),
	}

	app.isFirstStartupHTTP.Store(true)

	if config.Startup != nil {
		app.startFn = config.Startup
	}

	if len(app.port) == 0 {
		ln, err := net.Listen("tcp", ":0")
		if err != nil {
			return nil, err
		}

		_, port, err := net.SplitHostPort(ln.Addr().String())
		if err != nil {
			ln.Close()
			return nil, err
		}
		app.port = port
		app.ln = ln
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
	ErrUnknownUnit              = errors.New("unknown unit")
	ErrInvalidMaxBodySizeFormat = errors.New("invalid format")
)

func generateMaxBodySize(mbs MaxBodySize) (int, error) {
	s := strings.ReplaceAll(strings.TrimSpace(strings.ToUpper(mbs.String())), " ", "")

	if len(s) < 2 {
		return 0, ErrInvalidSize
	}

	var numPart, unitPart string
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

	value, err := strconv.Atoi(numPart)
	if err != nil {
		return 0, ErrInvalidMaxBodySizeFormat
	}

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
		return 0, ErrUnknownUnit
	}
}

func (a *App) Use(h Handler) {
	a.mw = append(a.mw, h)
}

func (a *App) GET(path string, h ...Handler) {
	a.root.addMethod("GET", split(path), h)
	a.registerOptions(path)
}

func (a *App) POST(path string, h ...Handler) {
	a.root.addMethod("POST", split(path), h)
	a.registerOptions(path)
}

func (a *App) PUT(path string, h ...Handler) {
	a.root.addMethod("PUT", split(path), h)
	a.registerOptions(path)
}

func (a *App) DELETE(path string, h ...Handler) {
	a.root.addMethod("DELETE", split(path), h)
	a.registerOptions(path)
}

func (a *App) PATCH(path string, h ...Handler) {
	a.root.addMethod("PATCH", split(path), h)
	a.registerOptions(path)
}

func (a *App) registerOptions(path string) {
	a.root.addMethod("OPTIONS", split(path), []Handler{
		func(c *Context) {
			c.SendStatus(http.StatusNoContent)
		},
	})
}

func (a *App) Listen() error {
	if a.ln == nil {
		ln, err := net.Listen("tcp", ":"+a.port)
		if err != nil {
			return err
		}
		a.ln = ln
	}

	a.startup()

	return a.acceptLoop(a.ln)
}

func (a *App) acceptLoop(ln net.Listener) error {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		// Bound concurrency: block until a slot is free so a flood of
		// connections cannot spawn unbounded goroutines/FDs.
		a.connSem <- struct{}{}
		a.activeConns.Add(1)
		go func() {
			defer func() {
				a.activeConns.Done()
				<-a.connSem
			}()
			a.serve(conn)
		}()
	}
}

// Shutdown gracefully shuts down the server: closes the listener and waits
// for active connections to finish, respecting the context deadline.
func (a *App) Shutdown(ctx context.Context) error {
	if a.ln == nil {
		return nil
	}

	err := a.ln.Close()

	done := make(chan struct{})
	go func() {
		a.activeConns.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		a.closeAllConns()
	}

	return err
}

func (a *App) startup() {
	if a.startFn != nil {
		a.startFn(a.port)
		return
	}
	a.log("http.server is running\n", fmt.Sprintf("http://localhost:%s\n", a.port))
}

func (a *App) trackConn(conn net.Conn, add bool) {
	a.mu.Lock()
	if add {
		a.conns[conn] = struct{}{}
	} else {
		delete(a.conns, conn)
	}
	a.mu.Unlock()
}

func (a *App) closeAllConns() {
	a.mu.Lock()
	for conn := range a.conns {
		conn.Close()
	}
	a.mu.Unlock()
}

// ServeFiles serves static files from the specified directory at the given endpoint.
// For example:
//
//	ServeFiles("/static/", "./public")
//
// will serve files from the "./public" directory at the "/static/" endpoint.
func (a *App) ServeFiles(endpoint, dirPath string) error {
	if len(endpoint) == 0 {
		endpoint = "/"
	}
	if endpoint[len(endpoint)-1] != '/' {
		endpoint += "/"
	}

	absDirPath, err := filepath.Abs(dirPath)
	if err != nil {
		return err
	}

	absDirPath = filepath.Clean(absDirPath) + string(filepath.Separator)

	a.GET(endpoint+":filename", func(c *Context) {
		filename := c.Param("filename")

		decodedFilename, err := url.PathUnescape(filename)
		if err != nil {
			c.SendStatus(http.StatusBadRequest)
			return
		}

		fullPath := filepath.Join(absDirPath, decodedFilename)

		if !strings.HasPrefix(fullPath, absDirPath) {
			c.SendStatus(http.StatusForbidden)
			return
		}

		c.SendFile(fullPath)
	})
	return nil
}

// ServeHTTP makes the app implement Go's http.Handler interface.
// This allows the app to be used in http.ListenAndServe.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if a.isFirstStartupHTTP.Load() {
		a.startup()
		a.isFirstStartupHTTP.Store(false)
	}
	ctx := newContext()
	ctx.appName = a.appName
	ctx.logger = a.logger
	ctx.w = w
	ctx.r = r
	ctx.isStdHTTP = true
	ctx.maxBodySize = a.maxBodySize

	ctx.method = []byte(r.Method)
	ctx.path = []byte(r.URL.Path)

	// The raw-socket parser fills ctx.query from the request line; without the
	// same step here, Context.Query always read empty when the app was mounted
	// as an http.Handler.
	if raw := r.URL.RawQuery; raw != "" {
		parseQueryString([]byte(raw), ctx)
	}

	// Store header keys lowercased so Context.Header lookups (which
	// lowercase the requested key) match, mirroring the raw-socket parser.
	for k, values := range r.Header {
		lk := strings.ToLower(k)
		for _, v := range values {
			ctx.header = append(ctx.header, KV{K: []byte(lk), V: []byte(v)})
		}
	}
	if r.Body != nil {
		limitReader := io.LimitReader(r.Body, int64(a.maxBodySize))
		ctx.body, _ = io.ReadAll(limitReader)
		r.Body.Close()
	}

	params := make([]KV, 0, 8)
	h, ok := a.root.findMethod(r.Method, splitBytes(ctx.path), &params)

	ctx.params = params
	ctx.handlers = append([]Handler{}, a.mw...)

	if ok {
		ctx.handlers = append(ctx.handlers, h...)
	} else if r.Method == "OPTIONS" {
		// Appended to the chain rather than answered inline: returning here
		// would skip a.mw, so a CORS middleware would never see the preflight
		// of an unregistered route. Mirrors serve().
		ctx.handlers = append(ctx.handlers, func(c *Context) {
			c.SendStatus(http.StatusNoContent)
		})
	} else {
		ctx.handlers = append(ctx.handlers, func(c *Context) {
			c.SendStatus(http.StatusNotFound)
		})
	}

	ctx.index = -1
	ctx.Next()

	ctx.finalizeNoBody()
}

func (a *App) serve(conn net.Conn) {
	a.trackConn(conn, true)
	defer func() {
		a.trackConn(conn, false)
		conn.Close()
	}()

	r := bufio.NewReader(conn)

	for {
		conn.SetReadDeadline(time.Now().Add(a.readTimeout))

		// A fresh Context per request: it must never be shared across
		// requests, so a handler retaining it (or a goroutine it spawned)
		// cannot observe a later request's data.
		ctx := newContext()
		ctx.appName = a.appName
		ctx.logger = a.logger
		ctx.conn = conn
		ctx.maxBodySize = a.maxBodySize

		result := parseRequest(r, ctx)
		switch result {
		case parseEOF:
			return
		case parseBadReq:
			ctx.SendStatus(http.StatusBadRequest)
			return
		case parseTooLarge:
			ctx.SendStatus(http.StatusRequestEntityTooLarge)
			return
		}

		params := make([]KV, 0, 8)
		h, ok := a.root.findMethod(string(ctx.method), splitBytes(ctx.path), &params)

		ctx.params = params
		ctx.handlers = append([]Handler{}, a.mw...)

		if ok {
			ctx.handlers = append(ctx.handlers, h...)
		} else if ctx.Method() == "OPTIONS" {
			ctx.handlers = append(ctx.handlers, func(c *Context) {
				c.SendStatus(http.StatusNoContent)
			})
		} else {
			ctx.handlers = append(ctx.handlers, func(c *Context) {
				c.SendStatus(http.StatusNotFound)
			})
		}

		ctx.index = -1
		ctx.Next()

		ctx.finalizeNoBody()

		if !keepAlive(ctx) {
			return
		}
	}
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
	if len(body) > 0 && (body[0] == '{' || body[0] == '[') && json.Valid(body) {
		return "application/json"
	}
	return http.DetectContentType(body)
}
