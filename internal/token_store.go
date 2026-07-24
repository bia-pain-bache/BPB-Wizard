package internal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	tokenStoreService   = "bpb-wizard"
	tokenStoreVersion   = 2
	backendKeyring      = "keyring"
	backendEncrypted    = "encrypted_file"
	argonTime           = uint32(3)
	argonMemory         = uint32(64 * 1024)
	argonThreads        = uint8(4)
	encryptionKeyLength = uint32(32)
	encryptionSaltSize  = 16
)

var (
	ErrMigrationDeclined = errors.New("token-store migration declined")
	ErrStoreReset        = errors.New("protected token store reset")
)

type cfLoginStore struct {
	Version     int              `json:"version"`
	ActiveEmail string           `json:"active_email"`
	Backend     string           `json:"backend"`
	Logins      []CfLogin        `json:"logins"`
	Encryption  *encryptionStore `json:"encryption,omitempty"`
}

type CfLogin struct {
	Email string `json:"email"`
	ID    string `json:"id"`
	Token string `json:"-"`
}

type encryptionStore struct {
	Salt    string                    `json:"salt"`
	Time    uint32                    `json:"time"`
	Memory  uint32                    `json:"memory_kib"`
	Threads uint8                     `json:"threads"`
	Tokens  map[string]encryptedToken `json:"tokens"`
}

type encryptedToken struct {
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type legacyStore struct {
	ActiveEmail string        `json:"active_email"`
	Logins      []legacyLogin `json:"logins"`
}

type legacyLogin struct {
	Email string `json:"email"`
	ID    string `json:"id"`
	Token string `json:"token"`
}

type migrationBackup struct {
	login       CfLogin
	previous    string
	hadPrevious bool
}

type passwordPrompter interface {
	ExistingPassword() ([]byte, error)
	NewPassword() ([]byte, error)
}

type tokenStorePrompts struct {
	confirmMigration func() bool
	confirmReset     func() bool
	password         passwordPrompter
}

type tokenStore struct {
	path       string
	vault      secretBackend
	prompts    tokenStorePrompts
	random     io.Reader
	sessionKey []byte
}

func NewTokenStore(logger *Logger) *tokenStore {
	return &tokenStore{
		path:  tokenFilePath(),
		vault: keyringSecretBackend{},
		prompts: tokenStorePrompts{
			confirmMigration: func() bool {
				logger.Info("Saved API tokens must be moved to protected storage before they can be used.")
				return ConfirmTokenMigration(logger)
			},
			confirmReset: func() bool {
				return ConfirmProtectedStoreReset(logger)
			},
			password: newTerminalPasswordPrompter(os.Stdin, os.Stdout),
		},
		random: rand.Reader,
	}
}

func newTokenStore(path string, vault secretBackend, prompts tokenStorePrompts, random io.Reader) *tokenStore {
	return &tokenStore{
		path:    path,
		vault:   vault,
		prompts: prompts,
		random:  random,
	}
}

func (s *tokenStore) LoadLogins() (cfLoginStore, error) {
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

	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return cfLoginStore{}, fmt.Errorf("decode token store: %w", err)
	}
	if header.Version == 0 {
		return s.migrateLegacyStore(data)
	}
	if header.Version != tokenStoreVersion {
		return cfLoginStore{}, fmt.Errorf("unsupported token store version %d", header.Version)
	}

	var store cfLoginStore
	if err := json.Unmarshal(data, &store); err != nil {
		return cfLoginStore{}, fmt.Errorf("decode token store: %w", err)
	}
	if err := validateStore(&store); err != nil {
		return cfLoginStore{}, err
	}
	if err := s.loadSecrets(&store); err != nil {
		return cfLoginStore{}, err
	}
	normalizeActiveLogin(&store)
	return store, nil
}

func (s *tokenStore) SaveLogin(login CfLogin) error {
	if strings.TrimSpace(login.Token) == "" {
		return errors.New("refusing to save an empty API token")
	}

	store, err := s.LoadLogins()
	if err != nil {
		return err
	}
	if store.Version == 0 {
		store, err = s.newProtectedStore()
		if err != nil {
			return err
		}
	}

	oldStore := cloneStore(store)
	oldToken, oldTokenErr := s.secretFor(&store, login.ID)
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
	store.ActiveEmail = login.Email

	if err := s.setSecret(&store, login.ID, login.Token); err != nil {
		return err
	}
	if err := s.writeStore(store); err != nil {
		s.rollbackSecret(&oldStore, login.ID, oldToken, oldTokenErr)
		return err
	}
	return nil
}

func (s *tokenStore) DeleteLogin(email string) (cfLoginStore, error) {
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

	removed := store.Logins[index]
	oldStore := cloneStore(store)
	store.Logins = append(store.Logins[:index], store.Logins[index+1:]...)
	if store.Encryption != nil {
		delete(store.Encryption.Tokens, removed.ID)
	}
	if store.ActiveEmail == email {
		store.ActiveEmail = ""
		if len(store.Logins) > 0 {
			store.ActiveEmail = store.Logins[0].Email
		}
	}

	if store.Backend == backendKeyring {
		if err := s.vault.Delete(removed.ID); err != nil && !errors.Is(err, ErrSecretNotFound) {
			return cfLoginStore{}, fmt.Errorf("delete API token from credential vault: %w", err)
		}
	}

	if len(store.Logins) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			s.restoreDeletedSecret(oldStore, removed)
			return cfLoginStore{}, fmt.Errorf("remove token store: %w", err)
		}
		s.clearSessionKey()
		return store, nil
	}
	if err := s.writeStore(store); err != nil {
		s.restoreDeletedSecret(oldStore, removed)
		return cfLoginStore{}, err
	}
	return store, nil
}

func (s *tokenStore) Reset() error {
	s.clearSessionKey()
	if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove protected token store: %w", err)
	}
	return nil
}

func (s *tokenStore) newProtectedStore() (cfLoginStore, error) {
	store := cfLoginStore{Version: tokenStoreVersion, Logins: []CfLogin{}}
	if err := probeSecretBackend(s.vault, s.random); err == nil {
		store.Backend = backendKeyring
		return store, nil
	}

	key, salt, err := s.newEncryptionKey()
	if err != nil {
		return cfLoginStore{}, err
	}
	s.sessionKey = key
	store.Backend = backendEncrypted
	store.Encryption = &encryptionStore{
		Salt:    base64.StdEncoding.EncodeToString(salt),
		Time:    argonTime,
		Memory:  argonMemory,
		Threads: argonThreads,
		Tokens:  make(map[string]encryptedToken),
	}
	return store, nil
}

func (s *tokenStore) loadSecrets(store *cfLoginStore) error {
	switch store.Backend {
	case backendKeyring:
		for i := range store.Logins {
			token, err := s.vault.Get(store.Logins[i].ID)
			if err != nil {
				return fmt.Errorf("read API token for %s from credential vault: %w", store.Logins[i].Email, err)
			}
			store.Logins[i].Token = token
		}
		return nil
	case backendEncrypted:
		return s.unlockEncryptedStore(store)
	default:
		return fmt.Errorf("unsupported token storage backend %q", store.Backend)
	}
}

func (s *tokenStore) unlockEncryptedStore(store *cfLoginStore) error {
	if len(s.sessionKey) > 0 {
		return s.decryptAll(store, s.sessionKey)
	}

	salt, err := decodeSalt(store.Encryption)
	if err != nil {
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		password, err := s.prompts.password.ExistingPassword()
		if err != nil {
			return err
		}
		key := deriveKey(password, salt, store.Encryption)
		clearBytes(password)
		if err := s.decryptAll(store, key); err == nil {
			s.sessionKey = key
			return nil
		}
		clearBytes(key)
	}

	if s.prompts.confirmReset != nil && s.prompts.confirmReset() {
		if err := s.Reset(); err != nil {
			return err
		}
		return ErrStoreReset
	}
	return errors.New("unable to unlock protected token store after three attempts")
}

func (s *tokenStore) decryptAll(store *cfLoginStore, key []byte) error {
	for i := range store.Logins {
		protected, ok := store.Encryption.Tokens[store.Logins[i].ID]
		if !ok {
			return fmt.Errorf("encrypted API token for %s is missing", store.Logins[i].Email)
		}
		token, err := decryptToken(key, protected, store.Logins[i].ID)
		if err != nil {
			return err
		}
		store.Logins[i].Token = token
	}
	return nil
}

func (s *tokenStore) setSecret(store *cfLoginStore, accountID, token string) error {
	switch store.Backend {
	case backendKeyring:
		if err := s.vault.Set(accountID, token); err != nil {
			return fmt.Errorf("save API token in credential vault: %w", err)
		}
		return nil
	case backendEncrypted:
		protected, err := encryptToken(s.sessionKey, token, accountID, s.random)
		if err != nil {
			return err
		}
		store.Encryption.Tokens[accountID] = protected
		return nil
	default:
		return fmt.Errorf("unsupported token storage backend %q", store.Backend)
	}
}

func (s *tokenStore) secretFor(store *cfLoginStore, accountID string) (string, error) {
	if store.Version == 0 {
		return "", ErrSecretNotFound
	}
	switch store.Backend {
	case backendKeyring:
		return s.vault.Get(accountID)
	case backendEncrypted:
		protected, ok := store.Encryption.Tokens[accountID]
		if !ok {
			return "", ErrSecretNotFound
		}
		return decryptToken(s.sessionKey, protected, accountID)
	default:
		return "", ErrSecretNotFound
	}
}

func (s *tokenStore) rollbackSecret(oldStore *cfLoginStore, accountID, oldToken string, oldErr error) {
	if oldStore.Version == 0 {
		_ = s.vault.Delete(accountID)
		return
	}
	if oldStore.Backend == backendEncrypted {
		return
	}
	if oldErr == nil {
		_ = s.vault.Set(accountID, oldToken)
	} else {
		_ = s.vault.Delete(accountID)
	}
}

func (s *tokenStore) restoreDeletedSecret(oldStore cfLoginStore, login CfLogin) {
	if oldStore.Backend == backendKeyring {
		_ = s.vault.Set(login.ID, login.Token)
	}
}

func (s *tokenStore) migrateLegacyStore(data []byte) (cfLoginStore, error) {
	var legacy legacyStore
	if err := json.Unmarshal(data, &legacy); err != nil {
		return cfLoginStore{}, fmt.Errorf("decode legacy token store: %w", err)
	}
	if len(legacy.Logins) == 0 {
		return cfLoginStore{}, errors.New("legacy token store contains no logins")
	}
	for _, login := range legacy.Logins {
		if login.Email == "" || login.ID == "" || login.Token == "" {
			return cfLoginStore{}, errors.New("legacy token store contains an incomplete login")
		}
	}
	if s.prompts.confirmMigration == nil || !s.prompts.confirmMigration() {
		return cfLoginStore{}, ErrMigrationDeclined
	}

	store, err := s.newProtectedStore()
	if err != nil {
		return cfLoginStore{}, err
	}
	store.ActiveEmail = legacy.ActiveEmail
	var migrated []migrationBackup
	for _, old := range legacy.Logins {
		login := CfLogin{Email: old.Email, ID: old.ID, Token: old.Token}
		backup := migrationBackup{login: login}
		if store.Backend == backendKeyring {
			previous, getErr := s.vault.Get(login.ID)
			switch {
			case getErr == nil:
				backup.previous = previous
				backup.hadPrevious = true
			case !errors.Is(getErr, ErrSecretNotFound):
				s.rollbackMigration(store, migrated)
				return cfLoginStore{}, fmt.Errorf("inspect existing credential for %s: %w", login.Email, getErr)
			}
		}
		if err := s.setSecret(&store, login.ID, login.Token); err != nil {
			s.rollbackMigration(store, migrated)
			return cfLoginStore{}, fmt.Errorf("migrate API token for %s: %w", login.Email, err)
		}
		migrated = append(migrated, backup)
		if store.Backend == backendKeyring {
			got, err := s.vault.Get(login.ID)
			if err != nil || got != login.Token {
				s.rollbackMigration(store, migrated)
				return cfLoginStore{}, fmt.Errorf("verify migrated API token for %s", login.Email)
			}
		}
		store.Logins = append(store.Logins, login)
	}
	normalizeActiveLogin(&store)

	if err := s.writeStore(store); err != nil {
		s.rollbackMigration(store, migrated)
		return cfLoginStore{}, err
	}
	return store, nil
}

func (s *tokenStore) rollbackMigration(store cfLoginStore, migrated []migrationBackup) {
	if store.Backend == backendKeyring {
		for _, backup := range migrated {
			if backup.hadPrevious {
				_ = s.vault.Set(backup.login.ID, backup.previous)
			} else {
				_ = s.vault.Delete(backup.login.ID)
			}
		}
	}
}

func (s *tokenStore) writeStore(store cfLoginStore) error {
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encode token store: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create token-store directory: %w", err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("protect token-store directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".store-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary token store: %w", err)
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return fmt.Errorf("protect temporary token store: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write temporary token store: %w", err)
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("sync temporary token store: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary token store: %w", err)
	}
	if err := replaceFile(tempPath, s.path); err != nil {
		return fmt.Errorf("replace token store: %w", err)
	}
	return nil
}

func (s *tokenStore) newEncryptionKey() ([]byte, []byte, error) {
	password, err := s.prompts.password.NewPassword()
	if err != nil {
		return nil, nil, err
	}
	defer clearBytes(password)

	salt := make([]byte, encryptionSaltSize)
	if _, err := io.ReadFull(s.random, salt); err != nil {
		return nil, nil, fmt.Errorf("generate encryption salt: %w", err)
	}
	params := &encryptionStore{Time: argonTime, Memory: argonMemory, Threads: argonThreads}
	return deriveKey(password, salt, params), salt, nil
}

func encryptToken(key []byte, token, accountID string, random io.Reader) (encryptedToken, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return encryptedToken{}, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(random, nonce); err != nil {
		return encryptedToken{}, fmt.Errorf("generate encryption nonce: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, []byte(token), tokenAAD(accountID))
	return encryptedToken{
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}, nil
}

func decryptToken(key []byte, protected encryptedToken, accountID string) (string, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	nonce, err := base64.StdEncoding.DecodeString(protected.Nonce)
	if err != nil || len(nonce) != aead.NonceSize() {
		return "", errors.New("invalid encrypted token nonce")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(protected.Ciphertext)
	if err != nil {
		return "", errors.New("invalid encrypted token ciphertext")
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, tokenAAD(accountID))
	if err != nil {
		return "", errors.New("incorrect password or damaged encrypted token")
	}
	return string(plaintext), nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initialize token encryption: %w", err)
	}
	return cipher.NewGCM(block)
}

func tokenAAD(accountID string) []byte {
	return []byte(fmt.Sprintf("%s:v%d:%s", tokenStoreService, tokenStoreVersion, accountID))
}

func deriveKey(password, salt []byte, params *encryptionStore) []byte {
	return argon2.IDKey(password, salt, params.Time, params.Memory, params.Threads, encryptionKeyLength)
}

func decodeSalt(store *encryptionStore) ([]byte, error) {
	if store == nil || store.Time != argonTime || store.Memory != argonMemory || store.Threads != argonThreads {
		return nil, errors.New("unsupported encrypted token-store parameters")
	}
	salt, err := base64.StdEncoding.DecodeString(store.Salt)
	if err != nil || len(salt) != encryptionSaltSize {
		return nil, errors.New("invalid encrypted token-store salt")
	}
	return salt, nil
}

func validateStore(store *cfLoginStore) error {
	if store.Backend != backendKeyring && store.Backend != backendEncrypted {
		return fmt.Errorf("unsupported token storage backend %q", store.Backend)
	}
	if len(store.Logins) == 0 {
		return errors.New("protected token store contains no logins")
	}
	seenIDs := make(map[string]bool)
	for _, login := range store.Logins {
		if login.Email == "" || login.ID == "" {
			return errors.New("protected token store contains an incomplete login")
		}
		if seenIDs[login.ID] {
			return fmt.Errorf("protected token store contains duplicate account ID %q", login.ID)
		}
		seenIDs[login.ID] = true
	}
	if store.Backend == backendEncrypted {
		if _, err := decodeSalt(store.Encryption); err != nil {
			return err
		}
	}
	return nil
}

func normalizeActiveLogin(store *cfLoginStore) {
	for _, login := range store.Logins {
		if login.Email == store.ActiveEmail {
			return
		}
	}
	if len(store.Logins) > 0 {
		store.ActiveEmail = store.Logins[0].Email
	}
}

func cloneStore(store cfLoginStore) cfLoginStore {
	clone := store
	clone.Logins = append([]CfLogin(nil), store.Logins...)
	if store.Encryption != nil {
		encryption := *store.Encryption
		encryption.Tokens = make(map[string]encryptedToken, len(store.Encryption.Tokens))
		for id, token := range store.Encryption.Tokens {
			encryption.Tokens[id] = token
		}
		clone.Encryption = &encryption
	}
	return clone
}

func (s *tokenStore) clearSessionKey() {
	clearBytes(s.sessionKey)
	s.sessionKey = nil
}

func clearBytes(value []byte) {
	for i := range value {
		value[i] = 0
	}
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
