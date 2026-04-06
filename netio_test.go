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
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if app != nil && app.appName != tt.wantAppName {
				t.Errorf("New() appName = %v, want %v", app.appName, tt.wantAppName)
			}
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

	if len(app.mw) != 1 {
		t.Errorf("Expected 1 middleware, got %d", len(app.mw))
	}
	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
		h, ok := app.root.findMethod(method, split("/"+strings.ToLower(method)), nil)
		if !ok || len(h) != 1 {
			t.Errorf("%s handler not registered correctly", method)
		}
	}
}

func TestServeFiles(t *testing.T) {
    dir := t.TempDir()
    f, err := os.Create(dir + "/test.txt")
    if err != nil {
        t.Fatalf("Failed to create test file: %v", err)
    }
    defer f.Close()
    msg := "Hello, NetIO!"
    if _, err = f.Write([]byte(msg)); err != nil {
        t.Fatalf("Failed to write to test file: %v", err)
    }
	
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
        if err != nil {
            t.Fatalf("Failed to make GET request: %v", err)
        }
        defer res.Body.Close()
        
        if res.StatusCode != http.StatusForbidden {
            t.Fatalf("Expected status 403 Forbidden, got %d", res.StatusCode)
        }
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

        if res.Code != http.StatusOK {
            t.Fatalf("Expected status 200, got %d", res.Code)
        }
        if res.Body.String() != msg {
            t.Fatalf("Expected body '%s', got '%s'", msg, res.Body.String())
        }
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
        if err != nil {
            t.Fatalf("Failed to make GET request: %v", err)
        }
        defer res.Body.Close()
        
        if res.StatusCode != http.StatusOK {
            t.Fatalf("Expected status 200, got %d", res.StatusCode)
        }
        
        body, err := io.ReadAll(res.Body)
        if err != nil {
            t.Fatalf("Failed to read response body: %v", err)
        }
        
        if string(body) != msg {
            t.Fatalf("Expected body '%s', got '%s'", msg, string(body))
        }
    })
}

func TestListen(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"}) 

	ln, err := net.Listen("tcp", ":"+app.port)
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		conn, _ := ln.Accept()
		go app.serve(conn)
		close(done)
	}()

	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	conn, err := net.Dial("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
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
		if (err != nil) != tt.err {
			t.Errorf("generateMaxBodySize(%q) error = %v, wantErr %v", tt.input, err, tt.err)
		}
		if err == nil && got != tt.want {
			t.Errorf("generateMaxBodySize(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestDetectContentType(t *testing.T) {
	jsonData := []byte(`{"foo":"bar"}`)
	textData := []byte("hello world")

	if detectContentType(jsonData) != "application/json" {
		t.Error("JSON content type detection failed")
	}
	if detectContentType(textData) == "application/json" {
		t.Error("Non-JSON detected as JSON")
	}
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
		if err != nil {
			t.Errorf("Shutdown returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Shutdown did not return in time")
	}
	_, err = ln.Accept()
	if err == nil {
		t.Error("expected listener to be closed")
	}
}
