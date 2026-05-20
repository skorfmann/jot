package storage

import "testing"

func TestFormatLimit(t *testing.T) {
	tests := []struct {
		name string
		n    int64
		want string
	}{
		{name: "bytes", n: 512, want: "512 B"},
		{name: "kilobytes", n: 1536, want: "1.5 KB"},
		{name: "megabytes", n: 10 * 1024 * 1024, want: "10.0 MB"},
		{name: "gigabytes", n: 3 * 1024 * 1024 * 1024, want: "3.0 GB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatLimit(tt.n); got != tt.want {
				t.Fatalf("FormatLimit(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}
