package internal

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	keyring "github.com/zalando/go-keyring"
)

var ErrSecretNotFound = errors.New("secret not found")

type secretBackend interface {
	Get(accountID string) (string, error)
	Set(accountID, token string) error
	Delete(accountID string) error
}

type keyringSecretBackend struct{}

func (keyringSecretBackend) Get(accountID string) (string, error) {
	token, err := keyring.Get(tokenStoreService, accountID)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrSecretNotFound
	}
	return token, err
}

func (keyringSecretBackend) Set(accountID, token string) error {
	return keyring.Set(tokenStoreService, accountID, token)
}

func (keyringSecretBackend) Delete(accountID string) error {
	err := keyring.Delete(tokenStoreService, accountID)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrSecretNotFound
	}
	return err
}

func probeSecretBackend(backend secretBackend, random io.Reader) error {
	probeBytes := make([]byte, 16)
	if _, err := io.ReadFull(random, probeBytes); err != nil {
		return fmt.Errorf("generate credential-vault probe: %w", err)
	}
	key := "probe-" + hex.EncodeToString(probeBytes)
	valueBytes := make([]byte, 16)
	if _, err := io.ReadFull(random, valueBytes); err != nil {
		return fmt.Errorf("generate credential-vault probe value: %w", err)
	}
	value := hex.EncodeToString(valueBytes)

	if err := backend.Set(key, value); err != nil {
		return err
	}
	cleanupNeeded := true
	defer func() {
		if cleanupNeeded {
			_ = backend.Delete(key)
		}
	}()
	got, err := backend.Get(key)
	if err != nil {
		return err
	}
	if got != value {
		return errors.New("credential-vault probe returned a different value")
	}
	if err := backend.Delete(key); err != nil {
		return err
	}
	cleanupNeeded = false
	return nil
}
