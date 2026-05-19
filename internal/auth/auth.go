package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"
)

const (
	SessionCookie = "jot_session"
	FlowCookie    = "jot_oauth"
)

type RawConfig struct {
	Mode         string        `yaml:"mode"`
	Issuer       string        `yaml:"issuer"`
	Audience     string        `yaml:"audience"`
	ClientID     string        `yaml:"client_id"`
	ClientSecret string        `yaml:"client_secret"`
	CookieSecret string        `yaml:"cookie_secret"`
	SessionTTL   time.Duration `yaml:"session_ttl"`
	Authorize    Rule          `yaml:"authorize"`
}

type Rule struct {
	RequiredClaims map[string]any `yaml:"required_claims" json:"required_claims,omitempty"`
	EmailDomains   []string       `yaml:"email_domains" json:"email_domains,omitempty"`
}

type Identity struct {
	Email  string         `json:"email"`
	Claims map[string]any `json:"claims,omitempty"`
}

type Authenticator struct {
	cfg       RawConfig
	provider  *oidc.Provider
	verifier  *oidc.IDTokenVerifier
	oauth     *oauth2.Config
	jwtSecret []byte
}

type contextKey struct{}

func New(ctx context.Context, cfg RawConfig, baseURL string) (*Authenticator, error) {
	if cfg.Mode == "" {
		cfg.Mode = "oidc"
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 8 * time.Hour
	}
	if cfg.Mode == "dev" {
		secret, err := decodeSecret(cfg.CookieSecret)
		if err != nil {
			return nil, err
		}
		return &Authenticator{cfg: cfg, jwtSecret: secret}, nil
	}
	if cfg.Issuer == "" || cfg.Audience == "" {
		return nil, errors.New("auth.issuer and auth.audience are required")
	}
	if cfg.ClientID == "" {
		cfg.ClientID = cfg.Audience
	}
	if cfg.CookieSecret == "" {
		return nil, errors.New("auth.cookie_secret is required")
	}
	secret, err := decodeSecret(cfg.CookieSecret)
	if err != nil {
		return nil, err
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	redirectURL, err := url.JoinPath(baseURL, "/_auth/callback")
	if err != nil {
		return nil, err
	}
	return &Authenticator{
		cfg:      cfg,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.Audience}),
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  redirectURL,
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		jwtSecret: secret,
	}, nil
}

func (a *Authenticator) ConfigResponse() map[string]any {
	if a.cfg.Mode == "dev" {
		return map[string]any{"mode": "dev", "scopes": []string{oidc.ScopeOpenID, "email", "profile"}}
	}
	return map[string]any{
		"issuer":    a.cfg.Issuer,
		"client_id": a.cfg.ClientID,
		"scopes":    []string{oidc.ScopeOpenID, "email", "profile"},
	}
}

func (a *Authenticator) Authenticate(r *http.Request) (Identity, error) {
	if hdr := r.Header.Get("Authorization"); strings.HasPrefix(hdr, "Bearer ") {
		return a.VerifyBearer(r.Context(), strings.TrimSpace(strings.TrimPrefix(hdr, "Bearer ")))
	}
	cookie, err := r.Cookie(SessionCookie)
	if err == nil {
		return a.VerifySession(cookie.Value)
	}
	return Identity{}, errors.New("missing credentials")
}

func (a *Authenticator) VerifyBearer(ctx context.Context, token string) (Identity, error) {
	if a.cfg.Mode == "dev" {
		if token != "dev" {
			return Identity{}, errors.New("invalid dev token")
		}
		id := Identity{Email: "dev@local", Claims: map[string]any{"email": "dev@local", "hd": "local"}}
		return id, nil
	}
	idToken, err := a.verifier.Verify(ctx, token)
	if err != nil {
		return Identity{}, err
	}
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return Identity{}, err
	}
	id := identityFromClaims(claims)
	if err := a.Check(id); err != nil {
		return Identity{}, err
	}
	return id, nil
}

func (a *Authenticator) VerifySession(raw string) (Identity, error) {
	token, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return a.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return Identity{}, errors.New("invalid session")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return Identity{}, errors.New("invalid session claims")
	}
	exp, err := claims.GetExpirationTime()
	if err != nil || exp == nil || time.Now().After(exp.Time) {
		return Identity{}, errors.New("expired session")
	}
	rawClaims := make(map[string]any, len(claims))
	for k, v := range claims {
		rawClaims[k] = v
	}
	id := identityFromClaims(rawClaims)
	if err := a.Check(id); err != nil {
		return Identity{}, err
	}
	return id, nil
}

func (a *Authenticator) Check(id Identity) error {
	if id.Email == "" {
		return errors.New("email claim is required")
	}
	if len(a.cfg.Authorize.EmailDomains) > 0 {
		parts := strings.Split(id.Email, "@")
		if len(parts) != 2 || !slices.Contains(a.cfg.Authorize.EmailDomains, parts[1]) {
			return fmt.Errorf("email domain is not authorized")
		}
	}
	for key, expected := range a.cfg.Authorize.RequiredClaims {
		actual, ok := id.Claims[key]
		if !ok {
			return fmt.Errorf("required claim %q missing", key)
		}
		if !claimMatches(actual, expected) {
			return fmt.Errorf("required claim %q does not match", key)
		}
	}
	return nil
}

func (a *Authenticator) WithIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := a.Authenticate(r)
		if err != nil {
			writeAuthError(w, r, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, id)))
	})
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(contextKey{}).(Identity)
	return id, ok
}

func (a *Authenticator) SignSession(id Identity, now time.Time) (string, error) {
	claims := jwt.MapClaims{
		"email": id.Email,
		"exp":   now.Add(a.cfg.SessionTTL).Unix(),
	}
	for k, v := range id.Claims {
		if k == "exp" {
			continue
		}
		claims[k] = v
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(a.jwtSecret)
}

func (a *Authenticator) LoginHandler(insecureHTTP bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		returnTo := r.URL.Query().Get("return_to")
		if returnTo == "" || !strings.HasPrefix(returnTo, "/") {
			returnTo = "/"
		}
		if a.cfg.Mode == "dev" {
			a.setSessionAndRedirect(w, r, Identity{Email: "dev@local", Claims: map[string]any{"email": "dev@local", "hd": "local"}}, returnTo, insecureHTTP)
			return
		}
		state, err := randomString(32)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		verifier := oauth2.GenerateVerifier()
		flowToken, err := signFlow(a.jwtSecret, state, verifier, returnTo)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		http.SetCookie(w, &http.Cookie{
			Name:     FlowCookie,
			Value:    flowToken,
			Path:     "/_auth",
			MaxAge:   600,
			HttpOnly: true,
			Secure:   !insecureHTTP,
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, a.oauth.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier)), http.StatusFound)
	}
}

func (a *Authenticator) CallbackHandler(insecureHTTP bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.cfg.Mode == "dev" {
			a.setSessionAndRedirect(w, r, Identity{Email: "dev@local", Claims: map[string]any{"email": "dev@local", "hd": "local"}}, "/", insecureHTTP)
			return
		}
		cookie, err := r.Cookie(FlowCookie)
		if err != nil {
			http.Error(w, "missing oauth flow cookie", http.StatusBadRequest)
			return
		}
		flow, err := verifyFlow(a.jwtSecret, cookie.Value)
		if err != nil {
			http.Error(w, "invalid oauth flow", http.StatusBadRequest)
			return
		}
		if r.URL.Query().Get("state") != flow.State {
			http.Error(w, "oauth state mismatch", http.StatusBadRequest)
			return
		}
		token, err := a.oauth.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(flow.Verifier))
		if err != nil {
			http.Error(w, "token exchange failed", http.StatusBadGateway)
			return
		}
		rawID, ok := token.Extra("id_token").(string)
		if !ok {
			http.Error(w, "id_token missing", http.StatusBadGateway)
			return
		}
		idToken, err := a.verifier.Verify(r.Context(), rawID)
		if err != nil {
			http.Error(w, "id_token verification failed", http.StatusUnauthorized)
			return
		}
		var claims map[string]any
		if err := idToken.Claims(&claims); err != nil {
			http.Error(w, "id_token claims failed", http.StatusUnauthorized)
			return
		}
		id := identityFromClaims(claims)
		if err := a.Check(id); err != nil {
			http.Error(w, "not authorized", http.StatusForbidden)
			return
		}
		a.setSessionAndRedirect(w, r, id, flow.ReturnTo, insecureHTTP)
	}
}

func (a *Authenticator) setSessionAndRedirect(w http.ResponseWriter, r *http.Request, id Identity, returnTo string, insecureHTTP bool) {
	session, err := a.SignSession(id, time.Now())
	if err != nil {
		http.Error(w, "could not sign session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    session,
		Path:     "/",
		MaxAge:   int(a.cfg.SessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   !insecureHTTP,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     FlowCookie,
		Value:    "",
		Path:     "/_auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   !insecureHTTP,
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, returnTo, http.StatusFound)
}

func (a *Authenticator) OAuthEndpoint() oauth2.Endpoint {
	if a.provider == nil {
		return oauth2.Endpoint{}
	}
	return a.provider.Endpoint()
}

func identityFromClaims(claims map[string]any) Identity {
	email, _ := claims["email"].(string)
	return Identity{Email: email, Claims: claims}
}

func claimMatches(actual any, expected any) bool {
	switch exp := expected.(type) {
	case []any:
		for _, one := range exp {
			if claimMatches(actual, one) {
				return true
			}
		}
		return false
	case []string:
		for _, one := range exp {
			if claimMatches(actual, one) {
				return true
			}
		}
		return false
	default:
		if actual == expected {
			return true
		}
		actualList, ok := actual.([]any)
		if !ok {
			return fmt.Sprint(actual) == fmt.Sprint(expected)
		}
		for _, item := range actualList {
			if fmt.Sprint(item) == fmt.Sprint(expected) {
				return true
			}
		}
		return false
	}
}

type flowClaims struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	ReturnTo string `json:"return_to"`
	Exp      int64  `json:"exp"`
	Mac      string `json:"mac"`
}

func signFlow(secret []byte, state, verifier, returnTo string) (string, error) {
	claims := flowClaims{
		State:    state,
		Verifier: verifier,
		ReturnTo: returnTo,
		Exp:      time.Now().Add(10 * time.Minute).Unix(),
	}
	payload := claims.State + "\n" + claims.Verifier + "\n" + claims.ReturnTo + "\n" + fmt.Sprint(claims.Exp)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	claims.Mac = hex.EncodeToString(mac.Sum(nil))
	body, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(body), nil
}

func verifyFlow(secret []byte, raw string) (flowClaims, error) {
	body, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return flowClaims{}, err
	}
	var claims flowClaims
	if err := json.Unmarshal(body, &claims); err != nil {
		return flowClaims{}, err
	}
	if time.Now().Unix() > claims.Exp {
		return flowClaims{}, errors.New("expired flow")
	}
	payload := claims.State + "\n" + claims.Verifier + "\n" + claims.ReturnTo + "\n" + fmt.Sprint(claims.Exp)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	if !hmac.Equal([]byte(claims.Mac), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		return flowClaims{}, errors.New("flow mac mismatch")
	}
	return claims, nil
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func decodeSecret(raw string) ([]byte, error) {
	if raw == "" {
		return nil, errors.New("auth.cookie_secret is required")
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("auth.cookie_secret must be hex: %w", err)
	}
	if len(decoded) < 32 {
		return nil, errors.New("auth.cookie_secret must be at least 32 random bytes")
	}
	return decoded, nil
}

func writeAuthError(w http.ResponseWriter, r *http.Request, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": msg})
}
