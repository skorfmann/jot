package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
)

const keyringService = "jot"

type Credential struct {
	Mode         string    `json:"mode,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	IDToken      string    `json:"id_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	TokenURL     string    `json:"token_url,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
}

func loadCredential(server string) (Credential, error) {
	if raw, err := keyring.Get(keyringService, server); err == nil {
		var cred Credential
		if err := json.Unmarshal([]byte(raw), &cred); err != nil {
			return Credential{}, err
		}
		return cred, nil
	}
	return loadCredentialFile(server)
}

func saveCredential(server string, cred Credential) error {
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	if err := keyring.Set(keyringService, server, string(raw)); err == nil {
		return nil
	}
	return saveCredentialFile(server, cred)
}

func deleteCredential(server string) error {
	_ = keyring.Delete(keyringService, server)
	creds, path, err := loadCredentialFileMap()
	if err != nil {
		return nil
	}
	delete(creds, server)
	body, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}

func loadCredentialFile(server string) (Credential, error) {
	creds, _, err := loadCredentialFileMap()
	if err != nil {
		return Credential{}, err
	}
	cred, ok := creds[server]
	if !ok {
		return Credential{}, errors.New("not logged in")
	}
	return cred, nil
}

func saveCredentialFile(server string, cred Credential) error {
	creds, path, err := loadCredentialFileMap()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if creds == nil {
		creds = map[string]Credential{}
	}
	creds[server] = cred
	body, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o600)
}

func loadCredentialFileMap() (map[string]Credential, string, error) {
	dir, err := configDir()
	if err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, "credentials.json")
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]Credential{}, path, nil
		}
		return nil, path, err
	}
	var creds map[string]Credential
	if err := json.Unmarshal(body, &creds); err != nil {
		return nil, path, err
	}
	return creds, path, nil
}
