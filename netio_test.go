package netio

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atendi9/capivara/assert"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name        string
		config      AppConfig
		wantAppName string
		wantErr     bool
	}{
		{
			name: "default app name",
			config: AppConfig{
				Port: "8080",
			},
			wantAppName: "netio",
			wantErr:     false,
		},
		{
			name: "custom app name",
			config: AppConfig{
				AppName:     "MyApp",
				Port:        "8080",
				MaxBodySize: "5 MB",
			},
			wantAppName: "MyApp",
			wantErr:     false,
		},
		{
			name: "invalid maxBodySize",
			config: AppConfig{
				Port:        "8080",
				MaxBodySize: "XYZ",
			},
			wantAppName: "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, err := New(tt.config)
			assert.False(t, (err != nil) != tt.wantErr)
			assert.False(t, app != nil && app.appName != tt.wantAppName)
		})
	}
}

func TestUseAndRoutes(t *testing.T) {
	app, _ := New(AppConfig{Port: "8080"})
	mw := Handler(func(c *Context) { c.Next() })
	app.Use(mw)

	handler := Handler(func(c *Context) {})
	app.GET("/get", handler)
	app.POST("/post", handler)
	app.PUT("/put", handler)
	app.DELETE("/delete", handler)
	app.PATCH("/patch", handler)

	assert.LengthSlice(t, 1, app.mw)

	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
		h, ok := app.root.findMethod(method, split("/"+strings.ToLower(method)), nil)
		assert.False(t, !ok || len(h) != 1)
	}
}

func TestServeFiles(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(dir + "/test.txt")
	assert.NoError(t, err)
	defer f.Close()
	msg := "Hello, NetIO!"
	_, err = f.Write([]byte(msg))
	assert.NoError(t, err)

	t.Run("Path traversal vulnerability using NetIO http.Server", func(t *testing.T) {
		portChan := make(chan string, 1)

		app, _ := New(AppConfig{Startup: func(p string) {
			portChan <- p
		}})

		app.ServeFiles("/static/", dir)
		go func() {
			app.Listen()
		}()

		port := <-portChan

		url := fmt.Sprintf("http://localhost:%s/static/..%%2f..%%2fetc%%2fpasswd", port)
		res, err := http.Get(url)
		assert.NoError(t, err)
		defer res.Body.Close()
		assertEqual(t, res.StatusCode, http.StatusForbidden)
	})

	t.Run("HTTP file serving using standard http.Server", func(t *testing.T) {
		var port string = "0"
		app, _ := New(AppConfig{Startup: func(p string) {
			port = p
		}})

		app.ServeFiles("/static/", dir)
		res := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost:%s/static/test.txt", port), nil)
		app.ServeHTTP(res, req)
		assertEqual(t, res.Code, http.StatusOK)
		assertEqual(t, res.Body.String(), msg)
	})

	t.Run("HTTP file serving using NetIO http.Server", func(t *testing.T) {
		portChan := make(chan string, 1)

		app, _ := New(AppConfig{Startup: func(p string) {
			portChan <- p
		}})

		app.ServeFiles("/static/", dir)
		go func() {
			app.Listen()
		}()

		port := <-portChan

		res, err := http.Get(fmt.Sprintf("http://localhost:%s/static/test.txt", port))
		assert.NoError(t, err)
		defer res.Body.Close()

		assertEqual(t, res.StatusCode, http.StatusOK)

		body, err := io.ReadAll(res.Body)
		assert.NoError(t, err)

		assertEqual(t, string(body), msg)
	})
}

func TestListen(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})

	ln, err := net.Listen("tcp", ":"+app.port)
	assert.NoError(t, err)
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		conn, _ := ln.Accept()
		go app.serve(conn)
		close(done)
	}()

	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	conn, err := net.Dial("tcp", "127.0.0.1:"+port)
	assert.NoError(t, err)
	conn.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Serve did not return in time")
	}
}

func TestGenerateMaxBodySize(t *testing.T) {
	tests := []struct {
		input string
		want  int
		err   bool
	}{
		{input: "10B", want: 10, err: false},
		{"1 KB", 1024, false},
		{"2 MB", 2 << 20, false},
		{"1 GB", 1 << 30, false},
		{"1TB", 1 << 40, false},
	}

	for _, tt := range tests {
		got, err := generateMaxBodySize(MaxBodySize(tt.input))
		assert.False(t, (err != nil) != tt.err)
		assert.False(t, err == nil && got != tt.want)
	}
}

func TestDetectContentType(t *testing.T) {
	jsonData := []byte(`{"foo":"bar"}`)
	textData := []byte("hello world")
	jsonContentType := "application/json"
	assertEqual(t, detectContentType(jsonData), jsonContentType)
	assertEqual(t, detectContentType(textData) == jsonContentType, false)
}

func TestShutdown(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})

	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	app.ln = ln

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)

	go func() {
		done <- app.Shutdown(ctx)
	}()

	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Shutdown did not return in time")
	}
	_, err = ln.Accept()
	assert.Error(t, err)
}
