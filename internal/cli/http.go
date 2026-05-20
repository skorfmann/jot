package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type apiClient struct {
	server string
	cred   Credential
	http   *http.Client
}

func newAPIClient(serverFlag string) (*apiClient, error) {
	server, err := resolveServer(serverFlag)
	if err != nil {
		return nil, err
	}
	cred, err := loadCredential(server)
	if err != nil {
		return nil, fmt.Errorf("not logged in to %s; run jot login --server %s", server, server)
	}
	return &apiClient{server: server, cred: cred, http: &http.Client{Timeout: 60 * time.Second}}, nil
}

func (c *apiClient) request(ctx context.Context, method, p string, in any, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.server+p, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	token, err := c.idToken(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("%s %s failed: %s: %s", method, p, res.Status, strings.TrimSpace(string(msg)))
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}

func (c *apiClient) idToken(ctx context.Context) (string, error) {
	if c.cred.Mode == "dev" {
		return "dev", nil
	}
	if c.cred.IDToken != "" && time.Until(c.cred.Expiry) > time.Minute {
		return c.cred.IDToken, nil
	}
	if c.cred.RefreshToken == "" || c.cred.TokenURL == "" || c.cred.ClientID == "" {
		return "", fmt.Errorf("stored credential cannot refresh; run jot login --server %s", c.server)
	}
	conf := oauth2.Config{
		ClientID:     c.cred.ClientID,
		ClientSecret: c.cred.ClientSecret,
		Endpoint: oauth2.Endpoint{
			TokenURL: c.cred.TokenURL,
		},
	}
	tok, err := conf.TokenSource(ctx, &oauth2.Token{RefreshToken: c.cred.RefreshToken}).Token()
	if err != nil {
		return "", err
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return "", fmt.Errorf("refresh response did not contain id_token")
	}
	c.cred.IDToken = raw
	c.cred.Expiry = tok.Expiry
	return raw, saveCredential(c.server, c.cred)
}
