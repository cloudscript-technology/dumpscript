package storage

import "testing"

func TestParseSize(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"1", 1, false},
		{"1024", 1024, false},
		{"1K", 1 << 10, false},
		{"1k", 1 << 10, false},
		{"512K", 512 << 10, false},
		{"100M", 100 << 20, false},
		{"200M", 200 << 20, false},
		{"1G", 1 << 30, false},
		{"2T", 2 << 40, false},
		{" 100M ", 100 << 20, false},
		{"", 0, true},
		{"abc", 0, true},
		{"10X", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseSize(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %d", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("parseSize(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}
