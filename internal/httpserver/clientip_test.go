package httpserver

import (
	"net/http"
	"testing"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		xRealIP    string
		xff        string
		remoteAddr string
		want       string
	}{
		{
			name:    "X-Real-Ip present",
			xRealIP: "1.2.3.4",
			want:    "1.2.3.4",
		},
		{
			name:    "X-Real-Ip with port",
			xRealIP: "1.2.3.4:9999",
			want:    "1.2.3.4",
		},
		{
			name:    "X-Real-Ip with spaces",
			xRealIP: " 10.0.0.1 ",
			want:    "10.0.0.1",
		},
		{
			name: "XFF single entry",
			xff:  "5.6.7.8",
			want: "5.6.7.8",
		},
		{
			name: "XFF multiple entries, first used",
			xff:  "5.6.7.8, 10.0.0.1, 192.168.1.1",
			want: "5.6.7.8",
		},
		{
			name: "XFF with spaces",
			xff:  " 5.6.7.8 , 10.0.0.1",
			want: "5.6.7.8",
		},
		{
			name:       "RemoteAddr fallback with port",
			remoteAddr: "192.168.1.100:54321",
			want:       "192.168.1.100",
		},
		{
			name:       "RemoteAddr fallback without port",
			remoteAddr: "192.168.1.100",
			want:       "192.168.1.100",
		},
		{
			name:       "RemoteAddr IPv6 with port",
			remoteAddr: "[::1]:8080",
			want:       "::1",
		},
		{
			name:    "X-Real-Ip takes priority over XFF",
			xRealIP: "1.1.1.1",
			xff:     "2.2.2.2",
			want:    "1.1.1.1",
		},
		{
			name:       "XFF takes priority over RemoteAddr",
			xff:        "3.3.3.3",
			remoteAddr: "4.4.4.4:1234",
			want:       "3.3.3.3",
		},
		{
			name: "all empty",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.xRealIP != "" {
				r.Header.Set("X-Real-Ip", tt.xRealIP)
			}
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			got := ClientIP(r)
			if got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}
