package netio

import (
	"fmt"
	"testing"

	"github.com/atendi9/capivara/assert"
)

func TestSplit(t *testing.T) {
	t.Run("slash only", func(t *testing.T) {
		got := split("/")
		assert.LengthSlice(t, 0, got)
	})

	t.Run("single segment", func(t *testing.T) {
		got := split("abc")
		want := [][]byte{{'b', 'c'}}
		assertEqual(t, want, got)
	})

	t.Run("one separator", func(t *testing.T) {
		got := split("a/bcd")
		want := [][]byte{{}, {'b', 'c', 'd'}}
		assertEqual(t, want, got)
	})
	t.Run("trailing separator", func(t *testing.T) {
		got := split("ab/")
		want := [][]byte{{'b'}, {}}
		assertEqual(t, want, got)
	})

	t.Run("leading separator after first char", func(t *testing.T) {
		got := split("a/bc")
		want := [][]byte{{}, {'b', 'c'}}
		assertEqual(t, want, got)
	})
}

func toStringSlices(slices [][]byte) [][]string {
	result := make([][]string, len(slices))
	for i, s := range slices {
		result[i] = make([]string, len(s))
		for j, b := range s {
			result[i][j] = string(b)
		}
	}
	return result
}

func assertEqual[T any](t testing.TB, want, got T) {
	assert.Equal(t, fmt.Sprint(want), fmt.Sprint(got))
}
