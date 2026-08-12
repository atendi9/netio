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

func TestContext_Logger_UsesAppLogger(t *testing.T) {
	var got []string
	c := newContext()
	c.logger = func(msgs ...string) { got = msgs }

	c.Logger()("hello", " world")

	if len(got) != 2 || got[0] != "hello" || got[1] != " world" {
		t.Errorf("expected app logger to receive the message, got %q", got)
	}
}

func TestContext_Logger_FallsBackWhenUnset(t *testing.T) {
	c := newContext()
	if c.Logger() == nil {
		t.Fatal("Logger must never return nil")
	}
	// Must not panic without an App-assigned logger.
	c.Logger()("fallback message")
}
