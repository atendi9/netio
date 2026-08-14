package netio

import (
	"bufio"
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
	appName     string
	port        string
	startFn     startFn
	logger      Logger
	root        *node
	mw          []Handler
	maxBodySize int
	readTimeout time.Duration
	// ln and closed are guarded by mu: Listen/ListenHTTPS publish the
	// listener from their own goroutine while Shutdown reads it from the
	// caller's.
	ln                 net.Listener
	closed             bool
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

// continueResponse is the interim response to Expect: 100-continue. It is
// written straight to the connection because an interim response precedes the
// real one rather than replacing it, so it must not mark the Context written.
var continueResponse = []byte("HTTP/1.1 100 Continue\r\n\r\n")

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
	a.root.addMethod("GET", routePattern(path), split(path), h)
	a.registerOptions(path)
}

func (a *App) POST(path string, h ...Handler) {
	a.root.addMethod("POST", routePattern(path), split(path), h)
	a.registerOptions(path)
}

func (a *App) PUT(path string, h ...Handler) {
	a.root.addMethod("PUT", routePattern(path), split(path), h)
	a.registerOptions(path)
}

func (a *App) DELETE(path string, h ...Handler) {
	a.root.addMethod("DELETE", routePattern(path), split(path), h)
	a.registerOptions(path)
}

func (a *App) PATCH(path string, h ...Handler) {
	a.root.addMethod("PATCH", routePattern(path), split(path), h)
	a.registerOptions(path)
}

// HEAD registers a handler for the HEAD method. Registering one is optional:
// RFC 7231 §4.3.2 defines HEAD as GET without the message body, so a HEAD
// request with no HEAD route of its own falls back to the GET handler and has
// its body suppressed. Register explicitly only to answer HEAD differently —
// to skip expensive work a GET would do, say.
func (a *App) HEAD(path string, h ...Handler) {
	a.root.addMethod("HEAD", routePattern(path), split(path), h)
	a.registerOptions(path)
}

// QUERY registers a handler for the QUERY method (RFC 10008). QUERY carries the
// query in the request content rather than the URI, and unlike POST it is both
// safe and idempotent, so a client or intermediary may retry it freely.
//
// RFC 10008 §2 requires the server to fail a QUERY whose Content-Type is missing
// or inconsistent with the content, so the missing case is rejected ahead of the
// supplied handlers. Deciding whether a present Content-Type actually matches
// the content is left to the handler, which is what knows the media type.
func (a *App) QUERY(path string, h ...Handler) {
	a.queryRoute(path, guardContentType(h))
}

// queryRoute registers an already-assembled QUERY chain. Group registration goes
// through here so it can place the guard after its own middlewares instead of
// ahead of them.
func (a *App) queryRoute(path string, handlers []Handler) {
	a.root.addMethod("QUERY", routePattern(path), split(path), handlers)
	a.registerOptions(path)
}

// guardContentType puts requireContentType immediately ahead of the handlers it
// protects, so every middleware registered before them still runs and any
// response header they set (CORS above all) survives a rejection.
func guardContentType(h []Handler) []Handler {
	guarded := make([]Handler, 0, len(h)+1)
	guarded = append(guarded, requireContentType)
	return append(guarded, h...)
}

// requireContentType aborts the chain with 400 when the request carries no
// Content-Type. RFC 10008 §2: "Servers MUST fail the request if the Content-Type
// request field is missing or is inconsistent with the request content."
func requireContentType(c *Context) {
	if c.Header("Content-Type") == "" {
		c.SendStatus(http.StatusBadRequest)
		c.Abort()
	}
}

func (a *App) registerOptions(path string) {
	a.root.addMethod("OPTIONS", routePattern(path), split(path), []Handler{
		func(c *Context) {
			c.SendStatus(http.StatusNoContent)
		},
	})
}

func (a *App) Listen() error {
	ln, err := a.bindListener()
	if err != nil {
		return err
	}

	if !a.setListener(ln) {
		ln.Close()
		return net.ErrClosed
	}

	a.startup(schemeHTTP)

	return a.acceptLoop(ln)
}

// bindListener returns the listener to accept on: the one New reserved when no
// port was configured, or a fresh one bound to a.port. Reusing the reserved
// listener is what keeps the auto-port path from binding the same port twice.
func (a *App) bindListener() (net.Listener, error) {
	ln := a.listener()
	if ln == nil {
		var err error
		if ln, err = net.Listen("tcp", ":"+a.port); err != nil {
			return nil, err
		}
	}

	// Port "0" asks the OS to choose one. Without reading the bound address
	// back, a.port keeps the literal "0" and the startup callback reports a
	// port nothing is listening on.
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		ln.Close()
		return nil, err
	}
	a.port = port

	return ln, nil
}

// listener returns the active listener, or nil before the first bind and after
// Shutdown.
func (a *App) listener() net.Listener {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.ln
}

// setListener publishes ln as the active listener, reporting false when
// Shutdown already ran. Without that check a Shutdown landing before the bind
// returns nil while the server goes on accepting forever.
func (a *App) setListener(ln net.Listener) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.closed {
		return false
	}
	a.ln = ln

	return true
}

// lookup resolves a route, falling back from HEAD to the GET handler when no
// HEAD route is registered. RFC 7231 §4.3.2 makes HEAD identical to GET but for
// the body, so every GET route answers HEAD for free — a server that 404s HEAD
// breaks health checks and link checkers for no reason. The caller suppresses
// the body; only the routing is decided here.
func (a *App) lookup(method string, path []byte, params *[]KV) (*route, bool) {
	segments := splitBytes(path)

	if r, ok := a.root.findMethod(method, segments, params); ok {
		return r, true
	}
	if method != http.MethodHead {
		return nil, false
	}

	// findMethod appends as it walks, so a failed attempt can leave partial
	// params behind.
	*params = (*params)[:0]

	return a.root.findMethod(http.MethodGet, segments, params)
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
	a.mu.Lock()
	a.closed = true
	ln := a.ln
	a.ln = nil
	a.mu.Unlock()

	var err error
	if ln != nil {
		err = ln.Close()
	}

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

// Schemes the startup banner reports, so an HTTPS server does not print a
// http:// URL its own TLS listener rejects.
const (
	schemeHTTP  = "http"
	schemeHTTPS = "https"
)

func (a *App) startup(scheme string) {
	if a.startFn != nil {
		a.startFn(a.port)
		return
	}
	a.log(
		fmt.Sprintf("%s.server is running\n", scheme),
		fmt.Sprintf("%s://localhost:%s\n", scheme, a.port),
	)
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
		a.startup(schemeHTTP)
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

	if r.Method == http.MethodHead {
		ctx.suppressBody = true
	}

	params := make([]KV, 0, 8)
	matched, ok := a.lookup(r.Method, ctx.path, &params)

	ctx.params = params
	ctx.handlers = append([]Handler{}, a.mw...)

	if ok {
		ctx.route = matched.pattern
		ctx.handlers = append(ctx.handlers, matched.handlers...)
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

		result := parseRequestHead(r, ctx)
		if result == parseOK {
			// Answered before the body is read: a client that sent
			// Expect: 100-continue is waiting for this and will not upload
			// until it arrives.
			switch result = expectation(ctx); result {
			case parseContinue:
				if _, err := conn.Write(continueResponse); err != nil {
					return
				}
				result = parseOK
			}
		}
		if result == parseOK {
			result = parseBody(r, ctx)
		}

		switch result {
		case parseEOF:
			return
		case parseBadReq:
			ctx.SendStatus(http.StatusBadRequest)
			return
		case parseTooLarge:
			ctx.SendStatus(http.StatusRequestEntityTooLarge)
			return
		case parseBadVersion:
			ctx.SendStatus(http.StatusHTTPVersionNotSupported)
			return
		case parseBadExpect:
			ctx.SendStatus(http.StatusExpectationFailed)
			return
		}

		if ctx.Method() == http.MethodHead {
			ctx.suppressBody = true
		}

		params := make([]KV, 0, 8)
		matched, ok := a.lookup(ctx.Method(), ctx.path, &params)

		ctx.params = params
		ctx.handlers = append([]Handler{}, a.mw...)

		if ok {
			ctx.route = matched.pattern
			ctx.handlers = append(ctx.handlers, matched.handlers...)
		} else if ctx.Method() == "OPTIONS" {
			ctx.handlers = append(ctx.handlers, func(c *Context) {
				c.SendStatus(http.StatusNoContent)
			})
		} else {
			ctx.handlers = append(ctx.handlers, func(c *Context) {
				c.SendStatus(http.StatusNotFound)
			})
		}

		// Decided before dispatch so it can be announced: RFC 7230 §6.6 asks a
		// server that is about to close to say so, which a handler can only do
		// if the header is in place before it writes.
		reuse := keepAlive(ctx)
		if !reuse {
			ctx.HeaderSet("Connection", "close")
		}

		ctx.index = -1
		ctx.Next()

		ctx.finalizeNoBody()

		if !reuse {
			return
		}
	}
}

func detectContentType(body []byte) string {
	if len(body) > 0 && (body[0] == '{' || body[0] == '[') && json.Valid(body) {
		return "application/json"
	}
	return http.DetectContentType(body)
}
