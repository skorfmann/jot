package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

type authConfigResponse struct {
	Mode     string   `json:"mode"`
	Issuer   string   `json:"issuer"`
	ClientID string   `json:"client_id"`
	Scopes   []string `json:"scopes"`
}

func (r *Root) loginCmd() *cobra.Command {
	var noBrowser bool
	var callbackPort int
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate to a jot server using OIDC",
		Example: `  jot login --server https://jot.example.com
  jot login --server http://localhost:8080
  jot login --server https://jot.example.com --callback-port 50573
  jot login --server https://jot.example.com --no-browser`,
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := resolveServer(r.serverURL)
			if err != nil {
				return err
			}
			cfg, err := fetchAuthConfig(cmd.Context(), server)
			if err != nil {
				return err
			}
			if cfg.Mode == "dev" {
				cred := Credential{Mode: "dev"}
				if err := saveCredential(server, cred); err != nil {
					return err
				}
				current, _ := loadConfig()
				current.DefaultServer = server
				_ = saveConfig(current)
				r.printf("Logged in to %s as dev@local\n", server)
				return nil
			}
			if cfg.Issuer == "" || cfg.ClientID == "" {
				return errors.New("server auth config is missing issuer or client_id")
			}
			provider, err := oidc.NewProvider(cmd.Context(), cfg.Issuer)
			if err != nil {
				return err
			}
			scopes := cfg.Scopes
			if len(scopes) == 0 {
				scopes = []string{oidc.ScopeOpenID, "email", "profile"}
			}
			oauthCfg := oauth2.Config{
				ClientID: cfg.ClientID,
				Scopes:   scopes,
				Endpoint: provider.Endpoint(),
			}
			var tok *oauth2.Token
			if noBrowser {
				tok, err = runDeviceFlow(cmd.Context(), r, oauthCfg)
			} else {
				tok, err = runLoopbackFlow(cmd.Context(), r, oauthCfg, callbackPort)
			}
			if err != nil {
				return err
			}
			raw, _ := tok.Extra("id_token").(string)
			if raw == "" {
				return errors.New("OIDC response did not include an id_token")
			}
			cred := Credential{
				RefreshToken: tok.RefreshToken,
				IDToken:      raw,
				Expiry:       tok.Expiry,
				TokenURL:     provider.Endpoint().TokenURL,
				ClientID:     cfg.ClientID,
			}
			if err := saveCredential(server, cred); err != nil {
				return err
			}
			current, _ := loadConfig()
			current.DefaultServer = server
			_ = saveConfig(current)
			r.printf("Logged in to %s\n", server)
			return nil
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Use OAuth 2.0 device authorization instead of opening a browser.")
	cmd.Flags().IntVar(&callbackPort, "callback-port", 50573, "Local callback port for browser login. Register http://127.0.0.1:PORT/callback with the OIDC provider; use 0 for a random port.")
	return cmd
}

func (r *Root) logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials for a jot server",
		Example: `  jot logout
  jot logout --server https://jot.example.com`,
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := resolveServer(r.serverURL)
			if err != nil {
				return err
			}
			if err := deleteCredential(server); err != nil {
				return err
			}
			r.printf("Logged out of %s\n", server)
			return nil
		},
	}
}

func fetchAuthConfig(ctx context.Context, server string) (authConfigResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server+"/_api/auth/config", nil)
	if err != nil {
		return authConfigResponse{}, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return authConfigResponse{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return authConfigResponse{}, fmt.Errorf("auth config request failed: %s", res.Status)
	}
	var cfg authConfigResponse
	return cfg, json.NewDecoder(res.Body).Decode(&cfg)
}

func runLoopbackFlow(ctx context.Context, r *Root, cfg oauth2.Config, callbackPort int) (*oauth2.Token, error) {
	addr := "127.0.0.1:0"
	if callbackPort > 0 {
		addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(callbackPort))
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w; pass --callback-port PORT and register http://127.0.0.1:PORT/callback with your OIDC provider", addr, err)
	}
	defer listener.Close()
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	state := fmt.Sprintf("%d", time.Now().UnixNano())
	verifier := oauth2.GenerateVerifier()
	cfg.RedirectURL = "http://" + listener.Addr().String() + "/callback"
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}
	mux.HandleFunc("/callback", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("state") != state {
			errCh <- errors.New("OAuth state mismatch")
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		code := req.URL.Query().Get("code")
		if code == "" {
			errCh <- errors.New("missing OAuth code")
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("jot login complete. You can close this tab."))
		codeCh <- code
	})
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	authURL := cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "consent"))
	if err := openBrowser(authURL); err != nil {
		r.printf("Open this URL in your browser:\n%s\n", authURL)
	} else {
		r.printf("Waiting for browser login...\n")
	}
	select {
	case code := <-codeCh:
		_ = server.Shutdown(context.Background())
		return cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	case err := <-errCh:
		_ = server.Shutdown(context.Background())
		return nil, err
	case <-time.After(5 * time.Minute):
		_ = server.Shutdown(context.Background())
		return nil, errors.New("login timed out")
	}
}

func runDeviceFlow(ctx context.Context, r *Root, cfg oauth2.Config) (*oauth2.Token, error) {
	device, err := cfg.DeviceAuth(ctx)
	if err != nil {
		return nil, err
	}
	if device.VerificationURIComplete != "" {
		r.printf("Open %s\n", device.VerificationURIComplete)
	} else {
		r.printf("Open %s and enter code %s\n", device.VerificationURI, device.UserCode)
	}
	return cfg.DeviceAccessToken(ctx, device)
}

func openBrowser(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	err := cmd.Start()
	if err != nil && strings.Contains(err.Error(), "executable file not found") {
		return err
	}
	return err
}
