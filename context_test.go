package netio

import (
	"bytes"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atendi9/capivara/assert"
	"github.com/atendi9/handlerx"
)

func TestContext_Status(t *testing.T) {
	c := &Context{}

	ret := c.Status(404)
	assertEqual(t, c.status, 404)
	assert.Equal(t, ret, c)
}

func TestContext_SendFile(t *testing.T) {
	tmp := t.TempDir()
	file := tmp + "/test.txt"

	err := os.WriteFile(file, []byte("hello"), 0644)
	assert.NoError(t, err)

	rw := httptest.NewRecorder()
	c := &Context{
		conn:   &fakeConn{rw: rw},
		status: 200,
	}

	c.SendFile(file)
	ok := strings.Contains(rw.Body.String(), "hello")
	assert.True(t, ok)
}

func TestContext_SendFile_Error(t *testing.T) {
	rw := httptest.NewRecorder()
	c := &Context{
		conn:   &fakeConn{rw: rw},
		status: 404,
	}

	c.SendFile("non-existent-file")

	assert.True(t, strings.Contains(rw.Body.String(), "404"))
}

func TestContext_SendFileFromReader(t *testing.T) {
	rw := httptest.NewRecorder()

	reader := io.NopCloser(strings.NewReader("stream-data"))

	c := &Context{
		conn:   &fakeConn{rw: rw},
		status: 200,
	}

	c.SendFileFromReader(reader)

	ok := strings.Contains(rw.Body.String(), "stream-data")
	assert.True(t, ok)
}

type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (e *errorReader) Close() error               { return nil }

func TestContext_SendFileFromReader_Error(t *testing.T) {
	rw := httptest.NewRecorder()

	c := &Context{
		conn:   &fakeConn{rw: rw},
		status: 500,
	}

	c.SendFileFromReader(&errorReader{})

	if !strings.Contains(rw.Body.String(), "500") {
		t.Errorf("expected status on copy error")
	}
}

func TestContext(t *testing.T) {
	_, ok := any(&Context{}).(handlerx.Context)
	if !ok {
		t.Fatal("not implemented")
	}
}

func TestContext_NextAndAbort(t *testing.T) {
	c := &Context{
		handlers: []Handler{
			func(c *Context) {
				c.header = append(c.header, KV{K: []byte("X-Test"), V: []byte("1")})
			},
			func(c *Context) {
				c.Abort()
			},
			func(c *Context) {
				c.Next()
			},
			func(c *Context) { c.header = append(c.header, KV{K: []byte("X-Test"), V: []byte("1")}) },
		},
		index: -1,
	}

	c.Next()
	assertEqual(t, c.Header("X-Test"), "1")
	assert.True(t, c.aborted)
}

func TestContext_reset(t *testing.T) {
	c := &Context{
		method: []byte("GET"),
		path:   []byte("/test"),
		status: 500,
		index:  5,
	}
	c.reset()
	ok := string(c.method) != "" || string(c.path) != "" || c.status != 200 || c.index != -1
	assert.False(t, ok)
}

func TestContext_HeadersAndHeader(t *testing.T) {
	jsonContentType := "application/json"
	c := &Context{
		header: []KV{
			{K: []byte("Content-Type"), V: []byte(jsonContentType)},
			{K: []byte("X-Test"), V: []byte("123")},
		},
	}
	headers := c.Headers()
	assertEqual(t, headers["Content-Type"][0], jsonContentType)
	assertEqual(t, c.Header("x-test"), "123")
}

func TestContext_MethodAndPathAndBody(t *testing.T) {
	c := &Context{
		method: []byte("POST"),
		path:   []byte("/hello"),
		body:   []byte(`{"a":1}`),
	}
	assertEqual(t, c.Method(), "POST")
	assertEqual(t, c.Path(), "/hello")
	ok := bytes.Equal(c.Body(), []byte(`{"a":1}`))
	assert.True(t, ok)
}

func TestContext_BodyParser(t *testing.T) {
	c := &Context{
		body: []byte(`{"name":"John"}`),
	}
	var data struct {
		Name string `json:"name"`
	}
	if err := c.BodyParser(&data); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "John", data.Name)
	cEmpty := &Context{}
	var d struct{}
	err := cEmpty.BodyParser(&d)
	assert.Equal(t, ErrEmptyBody, err)
}

func TestContext_QueryAndQueryParser(t *testing.T) {
	c := &Context{
		query: []KV{{K: []byte("foo"), V: []byte("bar")}},
	}
	assert.Equal(t, "bar", c.Query("foo"))
	assert.Equal(t, "default", c.Query("missing", "default"))

	var q struct {
		Foo string `query:"foo"`
	}
	if err := c.QueryParser(&q); err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, "bar", q.Foo)
}

func TestContext_ParamsAndParamsParser(t *testing.T) {
	c := &Context{
		params: []KV{{K: []byte("id"), V: []byte("42")}},
	}
	if c.Params("id") != "42" {
		t.Errorf("expected 42, got %s", c.Params("id"))
	}
	var p struct {
		ID string `param:"id"`
	}
	if err := c.ParamsParser(&p); err != nil {
		t.Fatal(err)
	}
	if p.ID != "42" {
		t.Errorf("params parser failed")
	}
}

func TestContext_ReqHeaderParser(t *testing.T) {
	c := &Context{
		header: []KV{{K: []byte("X-Foo"), V: []byte("bar")}},
	}
	var h struct {
		Foo string `header:"X-Foo"`
	}
	if err := c.ReqHeaderParser(&h); err != nil {
		t.Fatal(err)
	}
	if h.Foo != "bar" {
		t.Errorf("header parser failed")
	}
}

func TestContext_IPAndIPs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	host, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	addr := net.JoinHostPort(host, port)

	conn, _ := net.Dial("tcp", addr)
	c := &Context{conn: conn}

	if !strings.Contains(c.IP(), ".") {
		t.Errorf("IP parsing failed")
	}
	if len(c.IPs()) != 1 {
		t.Errorf("IPs parsing failed")
	}
}

func TestContext_SendAndJSON(t *testing.T) {
	rw := httptest.NewRecorder()
	c := &Context{
		conn: &fakeConn{rw: rw},
	}
	c.Send([]byte("hello"))
	if rw.Body.String() != "HTTP/1.1 200 OK\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: 5\r\n\r\nhello" {
		t.Logf("%q", rw.Body)
		t.Errorf("Send failed")
	}

	c.JSON(map[string]string{"a": "b"})
	if !strings.Contains(rw.Body.String(), `"a":"b"`) {
		t.Errorf("JSON failed")
	}
}

func TestContext_Now(t *testing.T) {
	c := &Context{}
	t1 := time.Now()
	t2 := c.Now()
	if t2.Before(t1) {
		t.Errorf("Now returned a time in the past")
	}
}

func TestContext_FormFile(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fw, _ := writer.CreateFormFile("file", "test.txt")
	io.WriteString(fw, "hello")
	writer.Close()

	c := &Context{
		body:   body.Bytes(),
		header: []KV{{K: []byte("Content-Type"), V: []byte(writer.FormDataContentType())}},
	}

	fh, err := c.FormFile("file")
	if err != nil {
		t.Fatal(err)
	}
	if fh.Filename != "test.txt" {
		t.Errorf("FormFile filename mismatch")
	}

	_, err = c.FormFile("missing")
	if err != ErrFormFileNotFound {
		t.Errorf("expected ErrFormFileNotFound")
	}
}

func TestContext_HeaderSet(t *testing.T) {
	c := &Context{}
	c.HeaderSet("X-Test", "123")
	if c.HeaderSet("X-Test", "456"); string(c.resHeader[0].V) != "456" {
		t.Errorf("HeaderSet failed")
	}
}

type fakeConn struct {
	rw *httptest.ResponseRecorder
}

func (f *fakeConn) Read(b []byte) (n int, err error)  { return 0, io.EOF }
func (f *fakeConn) Write(b []byte) (n int, err error) { return f.rw.Write(b) }
func (f *fakeConn) Close() error                      { return nil }
func (f *fakeConn) LocalAddr() net.Addr               { return nil }
func (f *fakeConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}
func (f *fakeConn) SetDeadline(t time.Time) error      { return nil }
func (f *fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *fakeConn) SetWriteDeadline(t time.Time) error { return nil }
