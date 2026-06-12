package netutil

import "testing"

func TestClientIP(t *testing.T) {
	tests := []struct {
		name     string
		remote   string
		xff      string
		xRealIP  string
		trusted  []string
		expected string
	}{
		{
			name:     "no trusted proxies ignores spoofed XFF",
			remote:   "203.0.113.7:54321",
			xff:      "1.2.3.4",
			trusted:  nil,
			expected: "203.0.113.7",
		},
		{
			name:     "untrusted peer ignores XFF even with proxies configured",
			remote:   "203.0.113.7:54321",
			xff:      "1.2.3.4",
			trusted:  []string{"10.0.0.0/8"},
			expected: "203.0.113.7",
		},
		{
			name:     "trusted peer returns rightmost-untrusted XFF address",
			remote:   "10.0.0.5:443",
			xff:      "9.9.9.9, 10.0.0.9, 10.0.0.5",
			trusted:  []string{"10.0.0.0/8"},
			expected: "9.9.9.9",
		},
		{
			name:     "trusted peer falls back to X-Real-IP",
			remote:   "10.0.0.5:443",
			xRealIP:  "8.8.8.8",
			trusted:  []string{"10.0.0.0/8"},
			expected: "8.8.8.8",
		},
		{
			name:     "bare IP remote without port",
			remote:   "203.0.113.7",
			trusted:  nil,
			expected: "203.0.113.7",
		},
		{
			name:     "IPv6 peer with port",
			remote:   "[2001:db8::1]:1234",
			trusted:  nil,
			expected: "2001:db8::1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClientIP(tt.remote, tt.xff, tt.xRealIP, tt.trusted)
			if got != tt.expected {
				t.Errorf("ClientIP(%q, %q, %q, %v) = %q, want %q",
					tt.remote, tt.xff, tt.xRealIP, tt.trusted, got, tt.expected)
			}
		})
	}
}
