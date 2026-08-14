package netio

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atendi9/handlerx"
)

func TestContext_ImplementsHandlerxContext(t *testing.T) {
	if _, ok := any(&Context{}).(handlerx.Context); !ok {
		t.Fatal("Context does not implement handlerx.Context")
	}
}

func TestContext_Next_NoHandlers(t *testing.T) {
	c := &Context{
		handlers: nil,
		index:    -1,
	}
	if err := c.Next(); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestContext_NextAndAbort(t *testing.T) {
	c := &Context{
		handlers: []Handler{
			func(c *Context) {
				c.header = append(c.header, KV{K: []byte("x-test"), V: []byte("1")})
			},
			func(c *Context) { c.Abort() },
			func(c *Context) { c.Next() },
			func(c *Context) {
				c.header = append(c.header, KV{K: []byte("x-test"), V: []byte("2")})
			},
		},
		index: -1,
	}

	c.Next()

	if c.Header("X-Test") != "1" {
		t.Errorf("expected X-Test=1, got %s", c.Header("X-Test"))
	}
	if !c.aborted {
		t.Error("expected context to be aborted")
	}
}

func TestContext_Reset(t *testing.T) {
	c := &Context{method: []byte("GET"), path: []byte("/test"), status: 500, index: 5}
	c.reset()
	if string(c.method) != "" || string(c.path) != "" || c.status != 200 || c.index != -1 {
		t.Error("context reset failed")
	}
}

func TestContext_Headers(t *testing.T) {
	c := &Context{
		header: []KV{
			{K: []byte("content-type"), V: []byte("application/json")},
			{K: []byte("x-test"), V: []byte("123")},
		},
	}
	if c.Headers()["content-type"][0] != "application/json" {
		t.Error("Headers map incorrect")
	}
	if c.Header("X-Test") != "123" {
		t.Error("Header case-insensitive lookup failed")
	}
}

func TestContext_Method(t *testing.T) {
	c := &Context{method: []byte("POST")}
	if c.Method() != "POST" {
		t.Errorf("expected POST, got %s", c.Method())
	}
}

func TestContext_Path(t *testing.T) {
	c := &Context{path: []byte("/hello")}
	if c.Path() != "/hello" {
		t.Errorf("expected /hello, got %s", c.Path())
	}
}

func TestContext_PathDefault(t *testing.T) {
	c := &Context{}
	if got := c.Path("default"); got != "default" {
		t.Errorf("expected default, got %s", got)
	}
	if got := c.Path(); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}

func TestContext_Body(t *testing.T) {
	c := &Context{body: []byte(`{"a":1}`)}
	if !bytes.Equal(c.Body(), []byte(`{"a":1}`)) {
		t.Error("body mismatch")
	}
}

// TestContext_Body_ReturnsCopy guards the critical data-corruption bug:
// Body() must not hand out the internal buffer, otherwise a handler that
// retains the result sees it mutated/zeroed when the Context is recycled or
// the buffer is reused by a later request on the same keep-alive connection.
func TestContext_Body_ReturnsCopy(t *testing.T) {
	c := &Context{body: []byte("original")}

	retained := c.Body()

	// Simulate the buffer being reused/truncated for a later request.
	c.body = c.body[:0]
	c.body = append(c.body, []byte("XXXXXXXX")...)

	if string(retained) != "original" {
		t.Errorf("Body() returned an aliased slice: retained value mutated to %q", retained)
	}
}

func TestContext_Body_Empty(t *testing.T) {
	c := &Context{}
	if c.Body() != nil {
		t.Errorf("expected nil body, got %q", c.Body())
	}
}

func TestContext_BodyParser(t *testing.T) {
	c := &Context{body: []byte(`{"Name":"John"}`)}
	var data struct{ Name string }
	if err := c.BodyParser(&data); err != nil || data.Name != "John" {
		t.Errorf("BodyParser failed: %v", err)
	}
}

func TestContext_BodyParser_Empty(t *testing.T) {
	c := &Context{}
	if err := c.BodyParser(&struct{}{}); err != ErrEmptyBody {
		t.Errorf("expected ErrEmptyBody, got %v", err)
	}
}

func TestContext_BodyParser_InvalidJSON(t *testing.T) {
	c := &Context{body: []byte(`invalid`)}
	if err := c.BodyParser(&struct{}{}); err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestContext_Query(t *testing.T) {
	c := &Context{query: []KV{{K: []byte("foo"), V: []byte("bar")}}}
	if c.Query("foo") != "bar" {
		t.Error("Query lookup failed")
	}
	if c.Query("missing", "default") != "default" {
		t.Error("Query default failed")
	}
	if c.Query("missing") != "" {
		t.Error("Query missing without default failed")
	}
}

func TestContext_QueryParser(t *testing.T) {
	c := &Context{query: []KV{{K: []byte("foo"), V: []byte("bar")}}}
	var q struct {
		Foo string `query:"foo"`
	}
	if err := c.QueryParser(&q); err != nil || q.Foo != "bar" {
		t.Errorf("QueryParser failed: %v", err)
	}
}

func TestContext_Params(t *testing.T) {
	c := &Context{params: []KV{{K: []byte("id"), V: []byte("42")}}}
	if c.Params("id") != "42" {
		t.Error("Params lookup failed")
	}
	if c.Params("missing", "fallback") != "fallback" {
		t.Error("Params default failed")
	}
	if c.Params("missing") != "" {
		t.Error("Params missing without default failed")
	}
}

func TestContext_Param(t *testing.T) {
	c := &Context{params: []KV{{K: []byte("id"), V: []byte("42")}}}
	if c.Param("id") != "42" {
		t.Errorf("expected 42, got %s", c.Param("id"))
	}
	if c.Param("missing") != "" {
		t.Errorf("expected empty, got %s", c.Param("missing"))
	}
}

func TestContext_ParamsParser(t *testing.T) {
	c := &Context{params: []KV{{K: []byte("id"), V: []byte("42")}}}
	var p struct {
		ID string `param:"id"`
	}
	if err := c.ParamsParser(&p); err != nil || p.ID != "42" {
		t.Errorf("ParamsParser failed: %v", err)
	}
}

func TestContext_Status(t *testing.T) {
	c := &Context{}
	if ret := c.Status(404); c.status != 404 || ret != c {
		t.Error("Status failed")
	}
}

func TestContext_SendFile(t *testing.T) {
	file := t.TempDir() + "/test.txt"
	if err := os.WriteFile(file, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	rw := httptest.NewRecorder()
	c := &Context{conn: &fakeConn{rw: rw}, status: 200}
	c.SendFile(file)
	if !strings.Contains(rw.Body.String(), "hello") {
		t.Error("expected file content to be sent")
	}
}

func TestContext_SendFile_NotFound(t *testing.T) {
	rw := httptest.NewRecorder()
	c := &Context{conn: &fakeConn{rw: rw}, status: 200}
	c.SendFile("non-existent-file")
	if !strings.Contains(rw.Body.String(), "404") {
		t.Error("expected 404 status on missing file")
	}
}

func TestContext_SendFile_ReadError(t *testing.T) {
	dir := t.TempDir()
	rw := httptest.NewRecorder()
	c := &Context{conn: &fakeConn{rw: rw}, status: 200}
	// Attempt to read a directory as a file — triggers non-IsNotExist error
	c.SendFile(dir)
	if !strings.Contains(rw.Body.String(), "500") {
		t.Error("expected 500 status on read error")
	}
}

func TestContext_SendFileFromReader(t *testing.T) {
	rw := httptest.NewRecorder()
	c := &Context{conn: &fakeConn{rw: rw}, status: 200}
	c.SendFileFromReader(io.NopCloser(strings.NewReader("stream-data")))
	if !strings.Contains(rw.Body.String(), "stream-data") {
		t.Error("expected streamed data")
	}
}

func TestContext_SendFileFromReader_Error(t *testing.T) {
	rw := httptest.NewRecorder()
	c := &Context{conn: &fakeConn{rw: rw}, status: 200}
	c.SendFileFromReader(&errorReader{})
	resp := rw.Body.String()
	if !strings.Contains(resp, "Transfer-Encoding: chunked") {
		t.Error("expected chunked transfer encoding in response")
	}
	if !strings.Contains(resp, "0\r\n\r\n") {
		t.Error("expected chunked terminator")
	}
}

func TestContext_ReqHeaderParser(t *testing.T) {
	c := &Context{header: []KV{{K: []byte("x-foo"), V: []byte("bar")}}}
	var h struct {
		Foo string `header:"x-foo"`
	}
	if err := c.ReqHeaderParser(&h); err != nil || h.Foo != "bar" {
		t.Errorf("ReqHeaderParser failed: %v", err)
	}
}

// Field names are case-insensitive (RFC 7230 §3.2) and the parser stores them
// lowercased, so a tag in any case must find the header the client sent. Binding
// the tag verbatim left `header:"apiKey"` empty on every request.
func TestContext_ReqHeaderParserIsCaseInsensitive(t *testing.T) {
	tags := []string{"apiKey", "APIKEY", "apikey", "ApiKey"}

	for _, tag := range tags {
		t.Run(tag, func(t *testing.T) {
			c := &Context{header: []KV{
				{K: []byte("apikey"), V: []byte("secret")},
				{K: []byte("content-type"), V: []byte("application/json")},
			}}

			dst := reflect.New(reflect.StructOf([]reflect.StructField{{
				Name: "ApiKey",
				Type: reflect.TypeOf(""),
				Tag:  reflect.StructTag(fmt.Sprintf("header:%q", tag)),
			}}))

			if err := c.ReqHeaderParser(dst.Interface()); err != nil {
				t.Fatalf("ReqHeaderParser(%q): %v", tag, err)
			}
			if got := dst.Elem().Field(0).String(); got != "secret" {
				t.Errorf("header %q bound to %q, want %q", tag, got, "secret")
			}
		})
	}
}

// The header travels the full path: parsed off the wire, then bound by a tag
// whose case does not match what the client sent.
func TestContext_ReqHeaderParserAfterParsing(t *testing.T) {
	raw := "GET /v1/budget HTTP/1.1\r\nHost: x\r\nApiKey: secret\r\nContent-Length: 0\r\n\r\n"

	c := newContext()
	if res := parseRequest(bufio.NewReader(strings.NewReader(raw)), c); res != parseOK {
		t.Fatalf("parseRequest = %v, want parseOK", res)
	}

	var header struct {
		ApiKey string `header:"apiKey"`
	}
	if err := c.ReqHeaderParser(&header); err != nil {
		t.Fatalf("ReqHeaderParser: %v", err)
	}
	if header.ApiKey != "secret" {
		t.Errorf("ApiKey = %q, want %q", header.ApiKey, "secret")
	}
	if got := c.Header("APIKEY"); got != "secret" {
		t.Errorf(`Header("APIKEY") = %q, want %q`, got, "secret")
	}
}

// A missing header still yields the zero value: case-insensitive lookup must not
// turn an absent header into a match on some other field.
func TestContext_ReqHeaderParserMissingHeader(t *testing.T) {
	c := &Context{header: []KV{{K: []byte("authorization"), V: []byte("Bearer x")}}}

	var header struct {
		ApiKey string `header:"apiKey"`
	}
	if err := c.ReqHeaderParser(&header); err != nil {
		t.Fatalf("ReqHeaderParser: %v", err)
	}
	if header.ApiKey != "" {
		t.Errorf("ApiKey = %q, want empty", header.ApiKey)
	}
}

// Query and param binding stay exact — only header names are case-insensitive.
func TestContext_ParserCaseSensitivityIsHeaderOnly(t *testing.T) {
	c := &Context{
		query:  []KV{{K: []byte("userId"), V: []byte("7")}},
		params: []KV{{K: []byte("budgetId"), V: []byte("9")}},
	}

	var q struct {
		UserID string `query:"userid"`
	}
	if err := c.QueryParser(&q); err != nil {
		t.Fatalf("QueryParser: %v", err)
	}
	if q.UserID != "" {
		t.Errorf("QueryParser matched %q case-insensitively, want no match", q.UserID)
	}

	var p struct {
		BudgetID string `param:"budgetid"`
	}
	if err := c.ParamsParser(&p); err != nil {
		t.Fatalf("ParamsParser: %v", err)
	}
	if p.BudgetID != "" {
		t.Errorf("ParamsParser matched %q case-insensitively, want no match", p.BudgetID)
	}
}

func TestContext_IP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	host, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	conn, _ := net.Dial("tcp", net.JoinHostPort(host, port))
	c := &Context{conn: conn}

	if !strings.Contains(c.IP(), ".") {
		t.Error("IP parsing failed")
	}
}

func TestContext_IP_FallbackOnBadAddr(t *testing.T) {
	c := &Context{conn: &badRemoteAddrConn{}}
	if ip := c.IP(); ip != "not-a-valid-addr" {
		t.Errorf("expected raw addr fallback, got %q", ip)
	}
}

func TestContext_IPs_FromConn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()

	host, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	conn, _ := net.Dial("tcp", net.JoinHostPort(host, port))
	c := &Context{conn: conn}

	if len(c.IPs()) != 1 {
		t.Error("expected 1 IP from conn")
	}
}

func TestContext_IPs_FromXForwardedFor(t *testing.T) {
	c := &Context{
		conn: &fakeConn{rw: httptest.NewRecorder()},
		header: []KV{
			{K: []byte("x-forwarded-for"), V: []byte("1.1.1.1, 2.2.2.2, 3.3.3.3")},
		},
	}
	ips := c.IPs()
	if len(ips) != 3 || ips[0] != "1.1.1.1" || ips[1] != "2.2.2.2" || ips[2] != "3.3.3.3" {
		t.Errorf("unexpected IPs: %v", ips)
	}
}

func TestContext_Send(t *testing.T) {
	rw := httptest.NewRecorder()
	c := &Context{conn: &fakeConn{rw: rw}}
	c.Send([]byte("hello"))
	if !strings.Contains(rw.Body.String(), "hello") {
		t.Error("Send failed")
	}
}

func TestContext_JSON(t *testing.T) {
	rw := httptest.NewRecorder()
	c := &Context{conn: &fakeConn{rw: rw}}
	if err := c.JSON(map[string]string{"a": "b"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rw.Body.String(), `"a":"b"`) {
		t.Error("JSON failed")
	}
}

func TestContext_JSON_MarshalError(t *testing.T) {
	rw := httptest.NewRecorder()
	c := &Context{conn: &fakeConn{rw: rw}}
	if err := c.JSON(make(chan int)); err == nil {
		t.Error("expected marshal error")
	}
}

func TestContext_Now(t *testing.T) {
	c := &Context{}
	t1 := time.Now()
	if c.Now().Before(t1) {
		t.Error("Now returned a time in the past")
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
		header: []KV{{K: []byte("content-type"), V: []byte(writer.FormDataContentType())}},
	}

	fh, err := c.FormFile("file")
	if err != nil || fh.Filename != "test.txt" {
		t.Errorf("FormFile failed: %v", err)
	}
	if _, err := c.FormFile("missing"); err != ErrFormFileNotFound {
		t.Errorf("expected ErrFormFileNotFound, got %v", err)
	}
}

func TestContext_FormFile_InvalidMultipart(t *testing.T) {
	c := &Context{
		body:   []byte("not multipart"),
		header: []KV{{K: []byte("content-type"), V: []byte("multipart/form-data; boundary=xxx")}},
	}
	if _, err := c.FormFile("file"); err == nil {
		t.Error("expected error for invalid multipart")
	}
}

func TestContext_FormFile_WithMaxBodySize(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fw, _ := writer.CreateFormFile("file", "test.txt")
	io.WriteString(fw, "hello")
	writer.Close()

	c := &Context{
		body:        body.Bytes(),
		maxBodySize: 1024,
		header:      []KV{{K: []byte("content-type"), V: []byte(writer.FormDataContentType())}},
	}

	fh, err := c.FormFile("file")
	if err != nil || fh.Filename != "test.txt" {
		t.Errorf("FormFile with maxBodySize failed: %v", err)
	}
}

func TestContext_FormFile_MissingContentType(t *testing.T) {
	c := &Context{body: []byte("data")}
	if _, err := c.FormFile("file"); err == nil {
		t.Error("expected error when Content-Type is missing")
	}
}

func TestContext_HeaderSet(t *testing.T) {
	c := &Context{}
	c.HeaderSet("X-Test", "123")
	c.HeaderSet("X-Test", "456")
	if string(c.resHeader[0].V) != "456" {
		t.Error("HeaderSet update failed")
	}
}

func TestContext_HeaderAppend(t *testing.T) {
	c := &Context{}
	c.HeaderAppend("Vary", "Origin")
	c.HeaderAppend("Vary", "Accept-Encoding")
	if string(c.resHeader[0].V) != "Origin, Accept-Encoding" {
		t.Errorf("expected 'Origin, Accept-Encoding', got %q", string(c.resHeader[0].V))
	}
}

func TestContext_HeaderAppend_New(t *testing.T) {
	c := &Context{}
	c.HeaderAppend("X-New", "value")
	if len(c.resHeader) != 1 || string(c.resHeader[0].V) != "value" {
		t.Error("HeaderAppend with new key failed")
	}
}

func TestContext_DoubleWrite(t *testing.T) {
	rw := httptest.NewRecorder()
	c := &Context{conn: &fakeConn{rw: rw}, status: 200}
	c.Send([]byte("first"))
	c.Send([]byte("second"))
	resp := rw.Body.String()
	// Should only contain one HTTP response
	if strings.Count(resp, "HTTP/1.1") != 1 {
		t.Errorf("expected single HTTP response, got multiple: %q", resp)
	}
}

func TestContext_MapToStruct_NonPointer(t *testing.T) {
	if err := mapToStruct(nil, "query", struct{ Name string }{}); err != ErrDstMustBeAPointer {
		t.Errorf("expected ErrDstMustBeAPointer, got %v", err)
	}
}

func TestContext_MapToStruct_NilPointer(t *testing.T) {
	var s *struct{ Name string }
	if err := mapToStruct(nil, "query", s); err != ErrDstMustBeAPointer {
		t.Errorf("expected ErrDstMustBeAPointer, got %v", err)
	}
}

func TestContext_MapToStruct_ScalarFields(t *testing.T) {
	var s struct {
		Count   int     `query:"count"`
		Enabled bool    `query:"enabled"`
		Ratio   float64 `query:"ratio"`
		Size    uint32  `query:"size"`
	}
	err := mapToStruct(map[string][]string{
		"count":   {"42"},
		"enabled": {"true"},
		"ratio":   {"1.5"},
		"size":    {"7"},
	}, "query", &s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Count != 42 || !s.Enabled || s.Ratio != 1.5 || s.Size != 7 {
		t.Errorf("unexpected struct: %+v", s)
	}
}

func TestContext_MapToStruct_InvalidScalar(t *testing.T) {
	var s struct {
		Count int `query:"count"`
	}
	if err := mapToStruct(map[string][]string{"count": {"notanumber"}}, "query", &s); err == nil {
		t.Error("expected error parsing invalid int, got nil")
	}
}

func TestContext_MapToStruct_UnsupportedField(t *testing.T) {
	var s struct {
		Tags []string `query:"tags"`
	}
	err := mapToStruct(map[string][]string{"tags": {"a"}}, "query", &s)
	if !errors.Is(err, ErrUnsupportedFieldType) {
		t.Errorf("expected ErrUnsupportedFieldType, got %v", err)
	}
}

func TestContext_MapToStruct_EmptyValue(t *testing.T) {
	var s struct {
		Name string `query:"name"`
	}
	if err := mapToStruct(map[string][]string{"other": {"x"}}, "query", &s); err != nil || s.Name != "" {
		t.Errorf("expected empty Name, got %q, err=%v", s.Name, err)
	}
}

func TestContext_MapToStruct_NoTag(t *testing.T) {
	var s struct {
		Name  string `query:"name"`
		Other string // no query tag — should be skipped
	}
	if err := mapToStruct(map[string][]string{"name": {"john"}}, "query", &s); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if s.Name != "john" || s.Other != "" {
		t.Errorf("expected Name=john, Other='', got Name=%q, Other=%q", s.Name, s.Other)
	}
}

func TestContext_MapToStruct_UnexportedField(t *testing.T) {
	var s struct {
		name string `query:"name"`
	}
	if err := mapToStruct(map[string][]string{"name": {"john"}}, "query", &s); err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if s.name != "" {
		t.Error("expected unexported field to be ignored")
	}
}

// Every numeric and boolean kind reports a parse failure instead of leaving the
// field at its zero value, which would silently hand a handler bad input.
func TestSetFieldValue_ParseErrors(t *testing.T) {
	var target struct {
		B   bool
		I   int
		U   uint
		F   float64
		Str string
	}
	v := reflect.ValueOf(&target).Elem()

	tests := []struct {
		name  string
		field string
		val   string
	}{
		{"bool", "B", "maybe"},
		{"int", "I", "12x"},
		{"uint", "U", "-1"},
		{"float", "F", "1.2.3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := setFieldValue(v.FieldByName(tt.field), tt.val); err == nil {
				t.Errorf("setFieldValue(%s, %q) = nil, want error", tt.field, tt.val)
			}
		})
	}

	if err := setFieldValue(v.FieldByName("Str"), "ok"); err != nil {
		t.Errorf("string field: %v", err)
	}
	if target.Str != "ok" {
		t.Errorf("Str = %q, want %q", target.Str, "ok")
	}
}

// Mounted as an http.Handler there is no raw conn to chunk onto, so the reader
// is copied straight into the ResponseWriter.
func TestContext_SendFileFromReader_StdHTTP(t *testing.T) {
	rw := httptest.NewRecorder()
	c := newContext()
	c.isStdHTTP = true
	c.w = rw
	c.status = http.StatusOK

	c.SendFileFromReader(io.NopCloser(strings.NewReader("stream-data")))

	if rw.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rw.Code)
	}
	if got := rw.Body.String(); got != "stream-data" {
		t.Errorf("body = %q, want %q", got, "stream-data")
	}
	if strings.Contains(rw.Header().Get("Transfer-Encoding"), "chunked") {
		t.Error("std http path must not hand-roll chunked framing")
	}
}

func TestContext_SendFileFromReader_StdHTTP_ReadError(t *testing.T) {
	rw := httptest.NewRecorder()
	c := newContext()
	c.isStdHTTP = true
	c.w = rw
	c.status = http.StatusOK

	c.SendFileFromReader(&errorReader{})

	if rw.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rw.Body.String())
	}
}

// Mounted as an http.Handler, IP() reads r.RemoteAddr, falling back to the raw
// value when it carries no port.
func TestContext_IP_StdHTTP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		want       string
	}{
		{"host and port", "203.0.113.7:54321", "203.0.113.7"},
		{"unparseable", "not-a-valid-addr", "not-a-valid-addr"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			c := newContext()
			c.isStdHTTP = true
			c.r = r

			if got := c.IP(); got != tt.want {
				t.Errorf("IP() = %q, want %q", got, tt.want)
			}
		})
	}
}

// A peer that disappears mid-stream must abort the chunked loop rather than
// keep writing into a dead connection. The first write is the header block, so
// each later index is one step of a chunk: size line, data, trailing CRLF.
func TestContext_SendFileFromReader_WriteErrors(t *testing.T) {
	for name, writes := range map[string]int{
		"fails on chunk size line": 1,
		"fails on chunk data":      2,
		"fails on chunk CRLF":      3,
	} {
		t.Run(name, func(t *testing.T) {
			conn := &failAfterNWrites{n: writes}
			c := newContext()
			c.conn = conn
			c.logger = func(msgs ...string) {}

			done := make(chan struct{})
			go func() {
				c.SendFileFromReader(io.NopCloser(strings.NewReader("stream-data")))
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("SendFileFromReader kept writing to a dead connection")
			}
		})
	}
}

// A path parameter arrives percent-escaped — a browser cannot put a space or a
// slash in a segment any other way — so a handler looking a record up by name
// or e-mail was searching for a string with "%20" still in it.
func TestContext_ParamsArePercentDecoded(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"space", "Minha%20Empresa", "Minha Empresa"},
		{"plus is literal in a path", "a+b", "a+b"},
		{"encoded at sign", "test%40test.com", "test@test.com"},
		{"nothing to decode", "plain", "plain"},
		{"encoded percent", "100%25", "100%"},
		{"malformed escape is kept verbatim", "%zz", "%zz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Context{params: []KV{{K: []byte("name"), V: []byte(tt.raw)}}}

			if got := c.Params("name"); got != tt.want {
				t.Errorf("Params(%q) = %q, want %q", tt.raw, got, tt.want)
			}
			if got := c.Param("name"); got != tt.want {
				t.Errorf("Param(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// The same decoding through the struct binder.
func TestContext_ParamsParserDecodes(t *testing.T) {
	c := &Context{params: []KV{{K: []byte("gmail"), V: []byte("test%40test.com")}}}

	var params struct {
		Gmail string `param:"gmail"`
	}
	if err := c.ParamsParser(&params); err != nil {
		t.Fatalf("ParamsParser: %v", err)
	}
	if params.Gmail != "test@test.com" {
		t.Errorf("Gmail = %q, want %q", params.Gmail, "test@test.com")
	}
}

// A missing parameter still falls back to the default rather than to "".
func TestContext_ParamsDefaultIsUnaffectedByDecoding(t *testing.T) {
	c := &Context{}

	if got := c.Params("missing", "fallback"); got != "fallback" {
		t.Errorf("Params default = %q, want %q", got, "fallback")
	}
	if got := c.Params("missing"); got != "" {
		t.Errorf("Params with no default = %q, want empty", got)
	}
}
