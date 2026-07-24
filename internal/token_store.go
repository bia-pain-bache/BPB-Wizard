package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const tokenStoreService = "bpb-wizard"

type cfLoginStore struct {
	ActiveEmail string    `json:"active_email"`
	Logins      []CfLogin `json:"logins"`
}

type CfLogin struct {
	Email string `json:"email"`
	ID    string `json:"id"`
	Token string `json:"token"`
}

type tokenStore struct {
	path string
}

func NewTokenStore() tokenStore {
	return tokenStore{path: tokenFilePath()}
}

func newTokenStore(path string) tokenStore {
	return tokenStore{path: path}
}

func (s tokenStore) LoadLogins() (cfLoginStore, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return cfLoginStore{}, nil
	}
	if err != nil {
		return cfLoginStore{}, fmt.Errorf("read token store: %w", err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return cfLoginStore{}, nil
	}

	var store cfLoginStore
	if err := json.Unmarshal(data, &store); err != nil {
		return cfLoginStore{}, fmt.Errorf("decode token store: %w", err)
	}
	if len(store.Logins) == 0 {
		store.ActiveEmail = ""
		return store, nil
	}
	activeFound := false
	for _, login := range store.Logins {
		if login.Email == store.ActiveEmail {
			activeFound = true
			break
		}
	}
	if !activeFound {
		store.ActiveEmail = store.Logins[0].Email
	}
	return store, nil
}

func (s tokenStore) SaveLogin(login CfLogin) error {
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

func (s tokenStore) DeleteLogin(email string) (cfLoginStore, error) {
	store, err := s.LoadLogins()
	if err != nil {
		return cfLoginStore{}, err
	}

	index := -1
	for i, login := range store.Logins {
		if login.Email == email {
			index = i
			break
		}
	}
	if index == -1 {
		return cfLoginStore{}, fmt.Errorf("saved account %q was not found", email)
	}

	store.Logins = append(store.Logins[:index], store.Logins[index+1:]...)
	if len(store.Logins) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return cfLoginStore{}, fmt.Errorf("remove token store: %w", err)
		}
		store.ActiveEmail = ""
		return store, nil
	}
	if store.ActiveEmail == email {
		store.ActiveEmail = store.Logins[0].Email
	}
	if err := s.createStore(store); err != nil {
		return cfLoginStore{}, err
	}
	return store, nil
}

func (s tokenStore) createStore(store cfLoginStore) error {
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	path := s.path
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
