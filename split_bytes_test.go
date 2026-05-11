package netio

import (
	"testing"
)

func TestSplitBytes(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		got := splitBytes([]byte{})
		assertEqual(t, [][]byte{}, got)
	})

	t.Run("single byte", func(t *testing.T) {
		got := splitBytes([]byte{'a'})
		assertEqual(t, [][]byte{}, got)
	})

	t.Run("no separator", func(t *testing.T) {
		got := splitBytes([]byte{'a', 'b', 'c'})
		want := [][]byte{{'b', 'c'}}
		assertEqual(t, want, got)
	})

	t.Run("one separator", func(t *testing.T) {
		got := splitBytes([]byte{'a', 'b', '/', 'c', 'd'})
		want := [][]byte{{'b'}, {'c', 'd'}}
		assertEqual(t, want, got)
	})

	t.Run("multiple separators", func(t *testing.T) {
		got := splitBytes([]byte{'x', '1', '/', '2', '/', '3'})
		want := [][]byte{{'1'}, {'2'}, {'3'}}
		assertEqual(t, want, got)
	})

	t.Run("trailing separator", func(t *testing.T) {
		got := splitBytes([]byte{'a', 'b', '/'})
		want := [][]byte{{'b'}, {}}
		assertEqual(t, want, got)
	})

	t.Run("leading separator after first byte", func(t *testing.T) {
		got := splitBytes([]byte{'a', '/', 'b', 'c'})
		want := [][]byte{{}, {'b', 'c'}}
		assertEqual(t, want, got)
	})
}
