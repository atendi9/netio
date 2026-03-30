package netio

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"
)

// Context represents a single HTTP request/response cycle.
type Context struct {
	conn net.Conn

	maxBodySize int

	method []byte
	path   []byte

	params    []KV
	query     []KV
	header    []KV
	resHeader []KV

	body []byte

	wrote bool

	appName string

	handlers []Handler
	index    int
	aborted  bool

	status int

	w         http.ResponseWriter
	r         *http.Request
	isStdHTTP bool
}

// KV represents a key-value pair.
type KV struct {
	K []byte
	V []byte
}

var ctxPool = sync.Pool{
	New: func() any {
		return &Context{
			params: make([]KV, 0, 8),
			query:  make([]KV, 0, 8),
			header: make([]KV, 0, 16),
		}
	},
}

var ErrAborted = errors.New("aborted")

// Next executes the next handler in the Context's handler chain.
func (c *Context) Next() error {
	c.index++
	for c.index < len(c.handlers) {
		c.handlers[c.index](c)
		if c.aborted {
			return ErrAborted
		}
		c.index++
	}
	return nil
}

// Abort stops the execution of the remaining handlers.
func (c *Context) Abort() { c.aborted = true }

func (c *Context) reset() {
	c.method = c.method[:0]
	c.path = c.path[:0]
	c.params = c.params[:0]
	c.query = c.query[:0]
	c.header = c.header[:0]
	c.resHeader = c.resHeader[:0]
	c.body = c.body[:0]
	c.handlers = nil
	c.index = -1
	c.aborted = false
	c.status = 200
	c.wrote = false
	c.isStdHTTP = false
}

// Headers returns all request headers as a map.
func (c *Context) Headers() map[string][]string {
	h := make(map[string][]string, len(c.header))
	for _, kv := range c.header {
		key := string(kv.K)
		h[key] = append(h[key], string(kv.V))
	}
	return h
}

// Header returns the first value for the given header key.
func (c *Context) Header(key string) string {
	key = strings.ToLower(key)
	for _, kv := range c.header {
		if strings.ToLower(string(kv.K)) == key {
			return string(kv.V)
		}
	}
	return ""
}

// Method returns the HTTP method of the request as a string.
func (c *Context) Method() string {
	return string(c.method)
}

// Path returns the request path, or a default value if empty.
func (c *Context) Path(defaultValue ...string) string {
	if len(c.path) > 0 {
		return string(c.path)
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

// Body returns the raw request body.
func (c *Context) Body() []byte {
	return c.body
}

var ErrEmptyBody = errors.New("empty body")

// BodyParser parses the request body JSON into the given destination.
func (c *Context) BodyParser(v any) error {
	if len(c.body) == 0 {
		return ErrEmptyBody
	}
	return json.Unmarshal(c.body, v)
}

// Query returns a query parameter value or a default if missing.
func (c *Context) Query(name string, defaultValue ...string) string {
	for _, kv := range c.query {
		if string(kv.K) == name {
			return string(kv.V)
		}
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

// QueryParser parses query parameters into a struct using `query` tags.
func (c *Context) QueryParser(v any) error {
	values := make(url.Values)
	for _, kv := range c.query {
		values.Add(string(kv.K), string(kv.V))
	}
	return mapToStruct(values, "query", v)
}

var ErrDstMustBeAPointer = errors.New("dst must be pointer")

func mapToStruct(values url.Values, tag string, dst any) error {
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return ErrDstMustBeAPointer
	}

	v = v.Elem()
	t := v.Type()

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		key := field.Tag.Get(tag)
		if key == "" {
			continue
		}

		val := values.Get(key)
		if val == "" {
			continue
		}

		f := v.Field(i)
		if !f.CanSet() {
			continue
		}

		switch f.Kind() {
		case reflect.String:
			f.SetString(val)
		}
	}

	return nil
}

// Params returns a path parameter value or a default if missing.
func (c *Context) Params(name string, defaultValue ...string) string {
	for _, kv := range c.params {
		if string(kv.K) == name {
			return string(kv.V)
		}
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

// Status sets the HTTP status code for the response.
//
// It returns the current Context instance to allow method chaining.
//
// Example:
//
//	ctx.Status(200).Send([]byte("ok"))
func (c *Context) Status(statusCode int) *Context {
	c.status = statusCode
	return c
}

// SendFile reads a file from the given path and sends its content as the response.
//
// If the file cannot be read, it sends the current status code without a body.
func (c *Context) SendFile(filePath string) {
	b, err := os.ReadFile(filePath)
	if err != nil {
		c.SendStatus(c.status)
		return
	}
	c.Send(b)
}

// SendFileFromReader streams data directly to the underlying connection.
//
// It uses io.Copy with net.Conn, which is efficient and avoids buffering the
// entire content in memory. Suitable for large payloads.
//
// The reader is closed after the operation.
func (c *Context) SendFileFromReader(r io.ReadCloser) {
	defer r.Close()
	if c.isStdHTTP {
		c.w.WriteHeader(c.status)
		_, err := io.Copy(c.w, r)
		if err != nil {
			c.SendStatus(c.status)
		}
		return
	}

	_, err := io.Copy(c.conn, r)
	if err != nil {
		c.SendStatus(c.status)
	}
}

// ParamsParser parses path parameters into a struct using `param` tags.
func (c *Context) ParamsParser(v any) error {
	values := make(url.Values)
	for _, kv := range c.params {
		values.Add(string(kv.K), string(kv.V))
	}
	return mapToStruct(values, "param", v)
}

// ReqHeaderParser parses headers into a struct using `header` tags.
func (c *Context) ReqHeaderParser(v any) error {
	values := make(url.Values)
	for _, kv := range c.header {
		values.Add(string(kv.K), string(kv.V))
	}
	return mapToStruct(values, "header", v)
}

// IP returns the remote IP address of the connection.
func (c *Context) IP() string {
	if c.isStdHTTP {
		host, _, err := net.SplitHostPort(c.r.RemoteAddr)
		if err != nil {
			return c.r.RemoteAddr
		}
		return host
	}
	host, _, err := net.SplitHostPort(c.conn.RemoteAddr().String())
	if err != nil {
		return c.conn.RemoteAddr().String()
	}
	return host
}

// IPs returns a slice of IPs from X-Forwarded-For or the remote IP.
func (c *Context) IPs() []string {
	xff := c.Header("X-Forwarded-For")
	if xff == "" {
		return []string{c.IP()}
	}

	parts := strings.Split(xff, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// SendStatus sends an HTTP response with the given status code.
func (c *Context) SendStatus(status int) error {
	c.status = status
	c.Send(nil)
	return nil
}

// Send writes raw data to the response.
func (c *Context) Send(data []byte) error {
	c.writeResponseWithHeaders(NewDefaultLogger(c.appName), c.status, data)
	return nil
}

// JSON sends a JSON response.
func (c *Context) JSON(data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	c.Send(b)
	return nil
}

// Now returns the current time.
func (c *Context) Now() time.Time {
	return time.Now()
}

// Param returns a single path parameter by key.
func (c *Context) Param(key string) string {
	for i := range c.params {
		if bytes.Equal(c.params[i].K, []byte(key)) {
			return string(c.params[i].V)
		}
	}
	return ""
}

var ErrFormFileNotFound = errors.New("form file not found")

// FormFile retrieves an uploaded file from a multipart/form request.
func (c *Context) FormFile(key string) (*multipart.FileHeader, error) {
	req, err := http.NewRequest(
		c.Method(),
		"/",
		bytes.NewReader(c.body),
	)
	if err != nil {
		return nil, err
	}

	for _, kv := range c.header {
		req.Header.Add(string(kv.K), string(kv.V))
	}

	maxMemory := int64(len(c.body))
	if c.maxBodySize > 0 {
		maxMemory = int64(c.maxBodySize)
	}

	if err := req.ParseMultipartForm(maxMemory); err != nil {
		return nil, err
	}

	files := req.MultipartForm.File[key]
	if len(files) > 0 {
		return files[0], nil
	}

	return nil, ErrFormFileNotFound
}

// HeaderSet sets or replaces a header in the Context.
func (c *Context) HeaderSet(key, value string) {
	lkey := strings.ToLower(key)
	for i := 0; i < len(c.resHeader); i++ {
		if strings.ToLower(string(c.resHeader[i].K)) == lkey {
			c.resHeader[i].V = []byte(value)
			return
		}
	}
	c.resHeader = append(c.resHeader, KV{K: []byte(key), V: []byte(value)})
}
