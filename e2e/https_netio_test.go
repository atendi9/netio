package e2e

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/atendi9/capivara/assert"
	"github.com/atendi9/netio/v2"
	"github.com/atendi9/netio/v2/cors"
)

func TestNetIOHTTPS(t *testing.T) {
	t.Helper()

	certPath, keyPath := generateTempCert(t)
	defer os.Remove(certPath)
	defer os.Remove(keyPath)

	portCh := make(chan string, 1)
	errCh := make(chan error, 1)

	runTestOnStartup := func(p string) {
		portCh <- p
	}

	app, err := netio.New(netio.AppConfig{
		Startup: runTestOnStartup,
		Port:    "0",
	})
	assert.NoError(t, err)

	allowedOrigins := []string{
		"https://google.com",
		"https://atendi9.com.br",
	}

	app.GET("/", func(c *netio.Context) {
		c.JSON(map[string]any{"message": "Hello World"})
	})

	app.Use(cors.Middleware(cors.Config{
    AllowOrigins: allowedOrigins,
    AllowMethods: []string{"GET", "POST", "OPTIONS"},
    AllowHeaders: []string{"*"},
    ExposeHeaders: []string{"*"},
    MaxAge:  600,
}))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Errorf("shutdown error: %v", err)
		}
	})

	go func() {
		if err := app.ListenHTTPS(certPath, keyPath); err != nil {
			errCh <- err
		}
	}()

	var port string
	select {
	case port = <-portCh:
	case err := <-errCh:
		t.Fatalf("HTTPS server failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for HTTPS server startup")
	}

	url := fmt.Sprintf("https://localhost:%s", port)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 5 * time.Second,
	}

	origin := "https://google.com"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Origin", origin)

	var res *http.Response
	for range 10 {
		res, err = client.Do(req)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	assert.NoError(t, err)
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	assert.NoError(t, err)

	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, `{"message":"Hello World"}`, string(body))
	got := res.Header.Get("Access-Control-Allow-Origin")
	assert.Equal(t, origin, got)
}

// generateTempCert generates a self-signed certificate and key and writes them to temp files.
func generateTempCert(t *testing.T) (certFile, keyFile string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(t, err)

	notBefore := time.Now()
	notAfter := notBefore.Add(24 * time.Hour)

	serialNumber, _ := rand.Int(rand.Reader, big.NewInt(1<<62))

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	assert.NoError(t, err)

	certFileObj, err := os.CreateTemp("", "cert-*.crt")
	assert.NoError(t, err)
	defer certFileObj.Close()

	keyFileObj, err := os.CreateTemp("", "key-*.key")
	assert.NoError(t, err)
	defer keyFileObj.Close()

	err = pem.Encode(certFileObj, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	assert.NoError(t, err)
	err = pem.Encode(keyFileObj, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})
	assert.NoError(t, err)

	return certFileObj.Name(), keyFileObj.Name()
}
