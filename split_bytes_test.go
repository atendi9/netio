package netio

import (
	"reflect"
	"testing"
)

func TestSplitBytes(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		got := splitBytes([]byte{})
		if got != nil {
			t.Errorf("SplitBytes([]) = %v, want nil", got)
		}
	})

	t.Run("single byte", func(t *testing.T) {
		got := splitBytes([]byte{'a'})
		if got != nil {
			t.Errorf("SplitBytes(['a']) = %v, want nil", got)
		}
	})

	t.Run("no separator", func(t *testing.T) {
		got := splitBytes([]byte{'a', 'b', 'c'})
		want := [][]byte{{'b', 'c'}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SplitBytes(['a','b','c']) = %v, want %v", got, want)
		}
	})

	t.Run("one separator", func(t *testing.T) {
		got := splitBytes([]byte{'a', 'b', '/', 'c', 'd'})
		want := [][]byte{{'b'}, {'c', 'd'}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SplitBytes(['a','b','/','c','d']) = %v, want %v", got, want)
		}
	})

	t.Run("multiple separators", func(t *testing.T) {
		got := splitBytes([]byte{'x', '1', '/', '2', '/', '3'})
		want := [][]byte{{'1'}, {'2'}, {'3'}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SplitBytes(['x','1','/','2','/','3']) = %v, want %v", got, want)
		}
	})

	t.Run("trailing separator", func(t *testing.T) {
		got := splitBytes([]byte{'a', 'b', '/'})
		want := [][]byte{{'b'}, {}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SplitBytes(['a','b','/']) = %v, want %v", got, want)
		}
	})

	t.Run("leading separator after first byte", func(t *testing.T) {
		got := splitBytes([]byte{'a', '/', 'b', 'c'})
		want := [][]byte{{}, {'b', 'c'}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("SplitBytes(['a','/','b','c']) = %v, want %v", got, want)
		}
	})
}