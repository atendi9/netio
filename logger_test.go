package netio

import (
	"strings"
	"testing"
)

func TestApp_NewMsg(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	msg := app.newMsg("hello")
	if !strings.Contains(msg, "hello") || !strings.Contains(msg, app.appName) {
		t.Errorf("unexpected format: %q", msg)
	}
}

func TestApp_Log_WithLogger(t *testing.T) {
	called := false
	app, _ := New(AppConfig{Port: "0", Logger: func(msgs ...string) { called = true }})
	app.log("x")
	if !called {
		t.Error("expected custom logger to be called")
	}
}

func TestApp_Log_WithoutLogger(t *testing.T) {
	app, _ := New(AppConfig{Port: "0"})
	app.logger = nil
	app.log("no logger")
}