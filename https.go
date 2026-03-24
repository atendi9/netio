package netio

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
)

// ErrInvalidCertKeyPaths is returned when either the certificate or key path is empty.
var ErrInvalidCertKeyPaths = errors.New("certPath and keyPath must be provided")

// ListenHTTPS starts an HTTPS server using the provided certificate and key files.
//
// Parameters:
//   - certPath: path to the PEM-encoded certificate file.
//   - keyPath: path to the PEM-encoded private key file.
//
// Usage:
//
//	app := netio.New(AppConfig{Port: "443"})
//	err := app.ListenHTTPS("server.crt", "server.key")
func (a *App) ListenHTTPS(certPath, keyPath string) error {
	if certPath == "" || keyPath == "" {
		return ErrInvalidCertKeyPaths
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return fmt.Errorf("failed to load certificate or key: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}
	ln, err := net.Listen("tcp", ":"+a.port)
	if err != nil {
		return fmt.Errorf("failed to listen on port %s: %w", a.port, err)
	}

	_, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		return fmt.Errorf("failed to parse listener address: %w", err)
	}
	a.port = portStr

	tlsListener := tls.NewListener(ln, tlsConfig)
	a.ln = tlsListener

	a.startup()

	for {
		conn, err := tlsListener.Accept()
		if err != nil {
			return fmt.Errorf("failed to accept connection: %w", err)
		}

		go a.serve(conn)
	}
}
