package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/oauth2"
)

const tokenStoreService = "Che-Wizard"

type cloudflareLoginStore struct {
	ActiveEmail string            `json:"active_email"`
	Logins      []cloudflareLogin `json:"logins"`
}

type cloudflareLogin struct {
	Email string        `json:"email"`
	Token *oauth2.Token `json:"token"`
}

type tokenStore struct{}

func newTokenStore() tokenStore {
	return tokenStore{}
}

func (s tokenStore) LoadLogins() (cloudflareLoginStore, error) {
	data, err := os.ReadFile(tokenFilePath())
	if err != nil {
		return cloudflareLoginStore{}, err
	}

	if strings.TrimSpace(string(data)) == "" {
		return cloudflareLoginStore{}, nil
	}

	var store cloudflareLoginStore
	if err := json.Unmarshal(data, &store); err == nil && len(store.Logins) > 0 {
		return store, nil
	}

	var token oauth2.Token
	if err := json.Unmarshal(data, &token); err != nil {
		return cloudflareLoginStore{}, nil
	}

	return cloudflareLoginStore{
		Logins: []cloudflareLogin{
			{
				Email: "Saved Cloudflare login",
				Token: &token,
			},
		},
	}, nil
}

func (s tokenStore) SaveLogin(login cloudflareLogin) error {
	store, err := s.LoadLogins()
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	store.ActiveEmail = login.Email

	replaced := false
	for i, item := range store.Logins {
		if item.Email == login.Email {
			store.Logins[i] = login
			replaced = true
			break
		}
	}

	if !replaced {
		store.Logins = append(store.Logins, login)
	}

	return s.saveLogins(store)
}

func (s tokenStore) DeleteLogin(email string) error {
	store, err := s.LoadLogins()
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	logins := store.Logins[:0]
	for _, login := range store.Logins {
		if login.Email != email {
			logins = append(logins, login)
		}
	}

	store.Logins = logins
	if store.ActiveEmail == email {
		store.ActiveEmail = ""
		if len(store.Logins) > 0 {
			store.ActiveEmail = store.Logins[0].Email
		}
	}

	if len(store.Logins) == 0 {
		return s.Delete()
	}

	return s.saveLogins(store)
}

func (s tokenStore) saveLogins(store cloudflareLoginStore) error {
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	path := tokenFilePath()

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return err
	}

	if err := os.Chmod(path, 0600); err != nil {
		return err
	}

	_ = os.Remove(legacyTokenFilePath())
	return nil
}

func (s tokenStore) Delete() error {
	err := os.Remove(tokenFilePath())
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	err = os.Remove(legacyTokenFilePath())
	if os.IsNotExist(err) {
		return nil
	}

	return err
}

func tokenFilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			dir = filepath.Join(home, ".config")
		} else {
			dir = "."
		}
	}

	return filepath.Join(dir, tokenStoreService, "tokens.json")
}

func legacyTokenFilePath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		if home, homeErr := os.UserHomeDir(); homeErr == nil {
			dir = filepath.Join(home, ".config")
		} else {
			dir = "."
		}
	}

	return filepath.Join(dir, tokenStoreService, "oauth-token.json")
}
