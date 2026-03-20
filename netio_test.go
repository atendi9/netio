package netio

import (
	"net"
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

func TestListen(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"}) // Porta 0 = porta livre do SO

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
