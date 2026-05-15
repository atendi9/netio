package netio

import (
	"io"
	"net"
	"net/http/httptest"
	"time"
)

// fakeConn implements net.Conn for testing, writing to an httptest.ResponseRecorder.
type fakeConn struct {
	rw *httptest.ResponseRecorder
}

func (f *fakeConn) Read(b []byte) (int, error)         { return 0, io.EOF }
func (f *fakeConn) Write(b []byte) (int, error)        { return f.rw.Write(b) }
func (f *fakeConn) Close() error                       { return nil }
func (f *fakeConn) LocalAddr() net.Addr                { return nil }
func (f *fakeConn) SetDeadline(t time.Time) error      { return nil }
func (f *fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *fakeConn) SetWriteDeadline(t time.Time) error { return nil }
func (f *fakeConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 12345}
}

// badRemoteAddrConn returns an unparseable remote address.
type badRemoteAddrConn struct{ fakeConn }

func (b *badRemoteAddrConn) RemoteAddr() net.Addr { return &badAddr{} }

type badAddr struct{}

func (b *badAddr) Network() string { return "tcp" }
func (b *badAddr) String() string  { return "not-a-valid-addr" }

// errorReader is an io.ReadCloser that always returns an error.
type errorReader struct{}

func (e *errorReader) Read(p []byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (e *errorReader) Close() error               { return nil }
