package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const tokenStoreService = "bpb-wizard"

type cfLoginStore struct {
	ActiveEmail string    `json:"active_email"`
	Logins      []cfLogin `json:"logins"`
}

type cfLogin struct {
	Email string `json:"email"`
	ID    string `json:"id"`
	Token string `json:"token"`
}

type tokenStore struct{}
func newTokenStore() tokenStore {
	return tokenStore{}
}

func (s tokenStore) LoadLogins() (cfLoginStore, error) {
	data, err := os.ReadFile(tokenFilePath())
	if err != nil || os.IsNotExist(err) {
		return cfLoginStore{}, nil
	}
	if strings.TrimSpace(string(data)) == "" {
		return cfLoginStore{}, nil
	}

	var store cfLoginStore
	if err := json.Unmarshal(data, &store); err == nil && len(store.Logins) > 0 {
		return store, nil
	}

	return cfLoginStore{
		ActiveEmail: "",
		Logins:      []cfLogin{},
	}, nil
}

func (s tokenStore) SaveLogin(login cfLogin) error {
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

	return s.createStore(store)
}

func (s tokenStore) createStore(store cfLoginStore) error {
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

	return nil
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

	return filepath.Join(dir, tokenStoreService, "store.json")
}
