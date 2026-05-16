package netio

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
)

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

	// statusSet records whether a handler explicitly called Status(): a
	// "handler forgot to write a body" fallback must honor that status
	// rather than overriding it with 204 No Content.
	statusSet bool

	appName string

	// logger is the app-configured logger, threaded onto the Context so
	// response writes reuse it instead of allocating a fresh logger per call
	// (and so a custom Logger passed to New is not silently bypassed).
	logger Logger

	handlers []Handler
	index    int
	aborted  bool

	status int

	w         http.ResponseWriter
	r         *http.Request
	isStdHTTP bool
}

type KV struct {
	K []byte
	V []byte
}

// newContext allocates a fresh per-request Context.
//
// Each request gets its own Context (rather than one recycled from a pool)
// so a handler that retains the *Context — or spawns a goroutine using it —
// is never exposed to data from a later request on the same keep-alive
// connection. The pre-sized slices keep the common case allocation-light.
func newContext() *Context {
	return &Context{
		params:    make([]KV, 0, 8),
		query:     make([]KV, 0, 8),
		header:    make([]KV, 0, 16),
		resHeader: make([]KV, 0, 8),
		status:    http.StatusOK,
		index:     -1,
	}
}

var ErrAborted = errors.New("aborted")

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
	c.statusSet = false
	c.wrote = false
	c.isStdHTTP = false
}

func (c *Context) Headers() map[string][]string {
	h := make(map[string][]string, len(c.header))
	for _, kv := range c.header {
		key := string(kv.K)
		h[key] = append(h[key], string(kv.V))
	}
	return h
}

func (c *Context) Header(key string) string {
	key = strings.ToLower(key)
	for _, kv := range c.header {
		if string(kv.K) == key {
			return string(kv.V)
		}
	}
	return ""
}

func (c *Context) Method() string {
	return string(c.method)
}

func (c *Context) Path(defaultValue ...string) string {
	if len(c.path) > 0 {
		return string(c.path)
	}
	if len(defaultValue) > 0 {
		return defaultValue[0]
	}
	return ""
}

// Body returns a copy of the request body.
//
// A copy is returned (rather than the internal buffer) so a handler may
// safely retain the result — or hand it to a spawned goroutine — without
// risking mutation by request parsing. Header/Params/Query lookups already
// return string copies and are likewise safe to retain.
func (c *Context) Body() []byte {
	if len(c.body) == 0 {
		return nil
	}
	cp := make([]byte, len(c.body))
	copy(cp, c.body)
	return cp
}

var ErrEmptyBody = errors.New("empty body")

func (c *Context) BodyParser(v any) error {
	if len(c.body) == 0 {
		return ErrEmptyBody
	}
	return json.Unmarshal(c.body, v)
}

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

func (c *Context) QueryParser(v any) error {
	values := make(url.Values)
	for _, kv := range c.query {
		values.Add(string(kv.K), string(kv.V))
	}
	return mapToStruct(values, "query", v)
}

var (
	ErrDstMustBeAPointer    = errors.New("dst must be pointer")
	ErrUnsupportedFieldType = errors.New("unsupported field type")
)

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

		if err := setFieldValue(f, val); err != nil {
			return fmt.Errorf("netio: field %q: %w", field.Name, err)
		}
	}

	return nil
}

// setFieldValue parses val into f according to f's kind. Unsupported kinds
// return an error rather than being silently skipped, so a caller binding an
// unparseable field is told instead of receiving a zero value.
func setFieldValue(f reflect.Value, val string) error {
	switch f.Kind() {
	case reflect.String:
		f.SetString(val)
	case reflect.Bool:
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}
		f.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(val, 10, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(val, 10, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(val, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetFloat(n)
	default:
		return ErrUnsupportedFieldType
	}
	return nil
}

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

func (c *Context) Status(statusCode int) *Context {
	c.status = statusCode
	c.statusSet = true
	return c
}

func (c *Context) SendFile(filePath string) {
	b, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			c.SendStatus(http.StatusNotFound)
		} else {
			c.SendStatus(http.StatusInternalServerError)
		}
		return
	}
	c.Send(b)
}

// SendFileFromReader streams a file from a reader, writing HTTP headers first
// and using Transfer-Encoding: chunked to avoid buffering the entire payload.
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

	c.HeaderSet("Transfer-Encoding", "chunked")
	c.writeResponseWithHeaders(c.responseLogger(), c.status, nil)

	buf := make([]byte, 32*1024)
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			if _, err := fmt.Fprintf(c.conn, "%x\r\n", n); err != nil {
				return
			}
			if _, err := c.conn.Write(buf[:n]); err != nil {
				return
			}
			if _, err := c.conn.Write([]byte("\r\n")); err != nil {
				return
			}
		}
		if readErr != nil {
			break
		}
	}
	c.conn.Write([]byte("0\r\n\r\n"))
}

func (c *Context) ParamsParser(v any) error {
	values := make(url.Values)
	for _, kv := range c.params {
		values.Add(string(kv.K), string(kv.V))
	}
	return mapToStruct(values, "param", v)
}

func (c *Context) ReqHeaderParser(v any) error {
	values := make(url.Values)
	for _, kv := range c.header {
		values.Add(string(kv.K), string(kv.V))
	}
	return mapToStruct(values, "header", v)
}

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

func (c *Context) SendStatus(status int) error {
	c.status = status
	return c.Send(nil)
}

// finalizeNoBody sends a fallback response when the handler chain produced no
// output. If a handler explicitly set a status it is honored (e.g. a 500 set
// without a body must not be downgraded to 204); otherwise 204 No Content is
// sent for the genuine "no content" case.
func (c *Context) finalizeNoBody() {
	if c.wrote {
		return
	}
	if c.statusSet {
		c.SendStatus(c.status)
		return
	}
	c.SendStatus(http.StatusNoContent)
}

// responseLogger returns the logger used for response logging, reusing the
// app-configured logger when available and falling back to a default one.
func (c *Context) responseLogger() Logger {
	if c.logger != nil {
		return c.logger
	}
	return NewDefaultLogger(c.appName)
}

func (c *Context) Send(data []byte) error {
	return c.writeResponseWithHeaders(c.responseLogger(), c.status, data)
}

func (c *Context) JSON(data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return c.Send(b)
}

func (c *Context) Now() time.Time {
	return time.Now()
}

// Param is an alias for Params without default value support.
func (c *Context) Param(key string) string {
	return c.Params(key)
}

var ErrFormFileNotFound = errors.New("form file not found")

func (c *Context) FormFile(key string) (*multipart.FileHeader, error) {
	req, _ := http.NewRequest(c.Method(), "/", bytes.NewReader(c.body))

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

func (c *Context) HeaderSet(key, value string) {
	lkey := strings.ToLower(key)
	for i := 0; i < len(c.resHeader); i++ {
		if strings.ToLower(string(c.resHeader[i].K)) == lkey {
			// Adopt the casing of the most recent set so the emitted header
			// does not keep a stale lowercase form from an earlier call site.
			c.resHeader[i].K = []byte(key)
			c.resHeader[i].V = []byte(value)
			return
		}
	}
	c.resHeader = append(c.resHeader, KV{K: []byte(key), V: []byte(value)})
}

func (c *Context) HeaderAppend(key, value string) {
	lkey := strings.ToLower(key)
	for i := 0; i < len(c.resHeader); i++ {
		if strings.ToLower(string(c.resHeader[i].K)) == lkey {
			c.resHeader[i].V = append(c.resHeader[i].V, ',', ' ')
			c.resHeader[i].V = append(c.resHeader[i].V, []byte(value)...)
			return
		}
	}
	c.resHeader = append(c.resHeader, KV{K: []byte(key), V: []byte(value)})
}
