package netio

import (
	"testing"
)

func TestApp_Log_WithLogger(t *testing.T) {
	called := false
	app, _ := New(AppConfig{Port: "0", Logger: func(msgs ...string) { called = true }})
	app.log("x")
	if !called {
		t.Error("expected custom logger to be called")
	}
}

func TestApp_Log_DefaultLogger(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	if app.logger == nil {
		t.Fatal("New must assign a default logger")
	}
	// Must not panic.
	app.log("default logger message")
}
