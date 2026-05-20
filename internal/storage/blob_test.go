package storage

import "testing"

func TestGoogleAccessIDFromCredentials(t *testing.T) {
	got := googleAccessIDFromCredentials([]byte(`{"client_email":"jot@example.iam.gserviceaccount.com"}`))
	if got != "jot@example.iam.gserviceaccount.com" {
		t.Fatalf("googleAccessIDFromCredentials = %q", got)
	}
}

func TestIsGCSEndpoint(t *testing.T) {
	tests := map[string]bool{
		"https://storage.googleapis.com": true,
		"storage.googleapis.com":         true,
		"https://s3.amazonaws.com":       false,
		"":                               false,
	}
	for endpoint, want := range tests {
		if got := isGCSEndpoint(endpoint); got != want {
			t.Fatalf("isGCSEndpoint(%q) = %v, want %v", endpoint, got, want)
		}
	}
}
