package auth

import "testing"

func TestAuthorizeRequiredClaims(t *testing.T) {
	a := &Authenticator{cfg: RawConfig{Authorize: Rule{RequiredClaims: map[string]any{"hd": "example.com"}}}}
	err := a.Check(Identity{Email: "alice@example.com", Claims: map[string]any{"email": "alice@example.com", "hd": "example.com"}})
	if err != nil {
		t.Fatalf("expected authorized: %v", err)
	}
	err = a.Check(Identity{Email: "mallory@example.net", Claims: map[string]any{"email": "mallory@example.net", "hd": "example.net"}})
	if err == nil {
		t.Fatal("expected authorization failure")
	}
}

func TestConfigResponseUsesCLIClientID(t *testing.T) {
	a := &Authenticator{cfg: RawConfig{Issuer: "https://accounts.example.com", ClientID: "web-client", CLIClientID: "cli-client", CLIClientSecret: "cli-secret"}}

	got := a.ConfigResponse()

	if got["client_id"] != "cli-client" {
		t.Fatalf("client_id = %v, want cli-client", got["client_id"])
	}
	if got["client_secret"] != "cli-secret" {
		t.Fatalf("client_secret = %v, want cli-secret", got["client_secret"])
	}
}

func TestConfigResponseFallsBackToBrowserClientID(t *testing.T) {
	a := &Authenticator{cfg: RawConfig{Issuer: "https://accounts.example.com", ClientID: "web-client"}}

	got := a.ConfigResponse()

	if got["client_id"] != "web-client" {
		t.Fatalf("client_id = %v, want web-client", got["client_id"])
	}
	if _, ok := got["client_secret"]; ok {
		t.Fatalf("client_secret is present: %v", got["client_secret"])
	}
}
