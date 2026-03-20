package netio

import "testing"

func TestKeepAlive(t *testing.T) {
	tests := []struct {
		name     string
		headers  []KV
		expected bool
	}{
		{
			name:     "Connection close",
			headers:  []KV{{K: []byte("Connection"), V: []byte("close")}},
			expected: false,
		},
		{
			name:     "Connection keep-alive",
			headers:  []KV{{K: []byte("Connection"), V: []byte("keep-alive")}},
			expected: true,
		},
		{
			name:     "Connection missing",
			headers:  []KV{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Context{header: tt.headers}
			got := keepAlive(c)
			if got != tt.expected {
				t.Errorf("keepAlive() = %v; want %v", got, tt.expected)
			}
		})
	}
}
