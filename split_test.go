package netio

import (
	"fmt"
	"reflect"
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

func TestSplitBytes(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		got := splitBytes([]byte{})
		if got != nil {
			t.Errorf("splitBytes([]) = %v, want nil", got)
		}
	})

	t.Run("single byte", func(t *testing.T) {
		got := splitBytes([]byte{'a'})
		if got != nil {
			t.Errorf("splitBytes(['a']) = %v, want nil", got)
		}
	})

	t.Run("no separator", func(t *testing.T) {
		got := splitBytes([]byte{'a', 'b', 'c'})
		want := [][]byte{{'b', 'c'}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("splitBytes(['a','b','c']) = %v, want %v", got, want)
		}
	})

	t.Run("one separator", func(t *testing.T) {
		got := splitBytes([]byte{'a', 'b', '/', 'c', 'd'})
		want := [][]byte{{'b'}, {'c', 'd'}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("splitBytes(['a','b','/','c','d']) = %v, want %v", got, want)
		}
	})

	t.Run("multiple separators", func(t *testing.T) {
		got := splitBytes([]byte{'x', '1', '/', '2', '/', '3'})
		want := [][]byte{{'1'}, {'2'}, {'3'}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("splitBytes(['x','1','/','2','/','3']) = %v, want %v", got, want)
		}
	})

	t.Run("trailing separator", func(t *testing.T) {
		got := splitBytes([]byte{'a', 'b', '/'})
		want := [][]byte{{'b'}, {}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("splitBytes(['a','b','/']) = %v, want %v", got, want)
		}
	})

	t.Run("leading separator after first byte", func(t *testing.T) {
		got := splitBytes([]byte{'a', '/', 'b', 'c'})
		want := [][]byte{{}, {'b', 'c'}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("splitBytes(['a','/','b','c']) = %v, want %v", got, want)
		}
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
