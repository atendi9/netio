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

	// Reuses the listener New reserved when no port was configured. Binding a
	// fresh one here would hit the port that listener already holds and fail
	// with EADDRINUSE, which is what Listen avoids by reusing it too.
	ln, err := a.bindListener()
	if err != nil {
		return fmt.Errorf("failed to listen on port %s: %w", a.port, err)
	}

	// Wrapping replaces the plain listener: closing the TLS one closes the
	// TCP listener underneath, so Shutdown still releases the port.
	tlsListener := tls.NewListener(ln, tlsConfig)
	if !a.setListener(tlsListener) {
		tlsListener.Close()
		return net.ErrClosed
	}

	a.startup(schemeHTTPS)

	return a.acceptLoop(tlsListener)
}
