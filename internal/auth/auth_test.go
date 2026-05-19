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
