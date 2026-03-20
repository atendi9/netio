package netio

import (
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	t.Run("slash only", func(t *testing.T) {
		got := split("/")
		if got != nil {
			t.Errorf("split(\"/\") = %v, want nil", got)
		}
	})

	t.Run("single segment", func(t *testing.T) {
		got := split("abc")
		want := [][]byte{{'b', 'c'}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("split(\"abc\") = %v, want %v", toStringSlices(got), toStringSlices(want))
		}
	})

	t.Run("one separator", func(t *testing.T) {
		got := split("a/bcd")
		want := [][]byte{{}, {'b', 'c', 'd'}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("split(\"a/bcd\") = %v, want %v", toStringSlices(got), toStringSlices(want))
		}
	})
	t.Run("trailing separator", func(t *testing.T) {
		got := split("ab/")
		want := [][]byte{{'b'}, {}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("split(\"ab/\") = %v, want %v", toStringSlices(got), toStringSlices(want))
		}
	})

	t.Run("leading separator after first char", func(t *testing.T) {
		got := split("a/bc")
		want := [][]byte{{}, {'b', 'c'}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("split(\"a/bc\") = %v, want %v", toStringSlices(got), toStringSlices(want))
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
