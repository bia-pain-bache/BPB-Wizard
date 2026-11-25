package internal

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testPassword = "correct horse battery staple"

type fakeSecretBackend struct {
	values    map[string]string
	failProbe bool
	failSet   map[string]error
	failGet   map[string]error
	failDel   map[string]error
}

func newFakeSecretBackend() *fakeSecretBackend {
	return &fakeSecretBackend{
		values:  make(map[string]string),
		failSet: make(map[string]error),
		failGet: make(map[string]error),
		failDel: make(map[string]error),
	}
}

func (f *fakeSecretBackend) Get(accountID string) (string, error) {
	if err := f.failGet[accountID]; err != nil {
		return "", err
	}
	value, ok := f.values[accountID]
	if !ok {
		return "", ErrSecretNotFound
	}
	return value, nil
}

func (f *fakeSecretBackend) Set(accountID, token string) error {
	if f.failProbe && strings.HasPrefix(accountID, "probe-") {
		return errors.New("vault unavailable")
	}
	if err := f.failSet[accountID]; err != nil {
		return err
	}
	f.values[accountID] = token
	return nil
}

func (f *fakeSecretBackend) Delete(accountID string) error {
	if err := f.failDel[accountID]; err != nil {
		return err
	}
	if _, ok := f.values[accountID]; !ok {
		return ErrSecretNotFound
	}
	delete(f.values, accountID)
	return nil
}

type fakePasswordPrompter struct {
	existing [][]byte
	newValue []byte
	newErr   error
}

func (p *fakePasswordPrompter) ExistingPassword() ([]byte, error) {
	if len(p.existing) == 0 {
		return nil, errors.New("unexpected existing-password prompt")
	}
	value := append([]byte(nil), p.existing[0]...)
	p.existing = p.existing[1:]
	return value, nil
}

func (p *fakePasswordPrompter) NewPassword() ([]byte, error) {
	if p.newErr != nil {
		return nil, p.newErr
	}
	return append([]byte(nil), p.newValue...), nil
}

func testStore(path string, vault secretBackend, passwords passwordPrompter) *tokenStore {
	return newTokenStore(path, vault, tokenStorePrompts{
		confirmMigration: func() bool { return true },
		confirmReset:     func() bool { return false },
		password:         passwords,
	}, rand.Reader)
}

func writeLegacyStore(t *testing.T, path string, logins []legacyLogin) []byte {
	t.Helper()
	legacy := legacyStore{ActiveEmail: logins[0].Email, Logins: logins}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestKeyringSaveLoadAndDelete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	vault := newFakeSecretBackend()
	store := testStore(path, vault, &fakePasswordPrompter{})
	login := CfLogin{Email: "one@example.com", ID: "account-one", Token: "secret-token"}

	if err := store.SaveLogin(login); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(login.Token)) {
		t.Fatal("plaintext token was written to store.json")
	}
	if vault.values[login.ID] != login.Token {
		t.Fatal("token was not saved in the credential vault")
	}

	loaded, err := testStore(path, vault, &fakePasswordPrompter{}).LoadLogins()
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Logins) != 1 || loaded.Logins[0].Token != login.Token {
		t.Fatalf("unexpected loaded logins: %#v", loaded.Logins)
	}

	remaining, err := store.DeleteLogin(login.Email)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.Logins) != 0 {
		t.Fatalf("expected no remaining logins, got %#v", remaining.Logins)
	}
	if _, ok := vault.values[login.ID]; ok {
		t.Fatal("token remained in credential vault after deletion")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected metadata file to be removed, stat error: %v", err)
	}
}

func TestDeleteLoginMaintainsActiveAccount(t *testing.T) {
	tests := []struct {
		name       string
		remove     string
		wantActive string
	}{
		{name: "remove non-active", remove: "one@example.com", wantActive: "two@example.com"},
		{name: "remove active", remove: "two@example.com", wantActive: "one@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "store.json")
			vault := newFakeSecretBackend()
			store := testStore(path, vault, &fakePasswordPrompter{})
			if err := store.SaveLogin(CfLogin{
				Email: "one@example.com",
				ID:    "account-one",
				Token: "token-one",
			}); err != nil {
				t.Fatal(err)
			}
			if err := store.SaveLogin(CfLogin{
				Email: "two@example.com",
				ID:    "account-two",
				Token: "token-two",
			}); err != nil {
				t.Fatal(err)
			}

			remaining, err := store.DeleteLogin(tt.remove)
			if err != nil {
				t.Fatal(err)
			}
			if remaining.ActiveEmail != tt.wantActive {
				t.Fatalf("got active email %q, want %q", remaining.ActiveEmail, tt.wantActive)
			}
			if len(remaining.Logins) != 1 {
				t.Fatalf("got %d remaining logins, want 1", len(remaining.Logins))
			}
		})
	}
}

func TestEncryptedFallbackRoundTripAndUniqueNonces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	vault := newFakeSecretBackend()
	vault.failProbe = true
	passwords := &fakePasswordPrompter{newValue: []byte(testPassword)}
	store := testStore(path, vault, passwords)

	first := CfLogin{Email: "one@example.com", ID: "account-one", Token: "same-token"}
	second := CfLogin{Email: "two@example.com", ID: "account-two", Token: "same-token"}
	if err := store.SaveLogin(first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveLogin(second); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(first.Token)) {
		t.Fatal("plaintext token was written to encrypted store")
	}
	var disk cfLoginStore
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Backend != backendEncrypted {
		t.Fatalf("got backend %q, want %q", disk.Backend, backendEncrypted)
	}
	if disk.Encryption.Tokens[first.ID].Nonce == disk.Encryption.Tokens[second.ID].Nonce {
		t.Fatal("encrypted tokens reused a nonce")
	}

	loader := testStore(path, vault, &fakePasswordPrompter{
		existing: [][]byte{[]byte(testPassword)},
	})
	loaded, err := loader.LoadLogins()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Logins[0].Token != first.Token || loaded.Logins[1].Token != second.Token {
		t.Fatalf("unexpected decrypted logins: %#v", loaded.Logins)
	}

	remaining, err := loader.DeleteLogin(first.Email)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining.Logins) != 1 {
		t.Fatalf("got %d remaining logins, want 1", len(remaining.Logins))
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(first.ID)) {
		t.Fatal("deleted account still has an encrypted token entry")
	}
}

func TestEncryptedStoreRejectsWrongPasswordAndCanReset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	vault := newFakeSecretBackend()
	vault.failProbe = true
	creator := testStore(path, vault, &fakePasswordPrompter{newValue: []byte(testPassword)})
	if err := creator.SaveLogin(CfLogin{
		Email: "one@example.com",
		ID:    "account-one",
		Token: "secret-token",
	}); err != nil {
		t.Fatal(err)
	}

	reset := testStore(path, vault, &fakePasswordPrompter{
		existing: [][]byte{[]byte("wrong-one"), []byte("wrong-two"), []byte("wrong-three")},
	})
	reset.prompts.confirmReset = func() bool { return true }
	if _, err := reset.LoadLogins(); !errors.Is(err, ErrStoreReset) {
		t.Fatalf("got error %v, want ErrStoreReset", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected reset to remove store, stat error: %v", err)
	}
}

func TestEncryptedTokenDetectsTampering(t *testing.T) {
	key := bytes.Repeat([]byte{0x42}, 32)
	protected, err := encryptToken(key, "secret-token", "account-one", bytes.NewReader(bytes.Repeat([]byte{1}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := base64Decode(protected.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[0] ^= 0xff
	protected.Ciphertext = base64Encode(ciphertext)
	if _, err := decryptToken(key, protected, "account-one"); err == nil {
		t.Fatal("expected modified ciphertext to fail authentication")
	}
}

func TestLegacyMigrationAccepted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	legacyToken := "legacy-plaintext-token"
	writeLegacyStore(t, path, []legacyLogin{
		{Email: "one@example.com", ID: "account-one", Token: legacyToken},
	})
	vault := newFakeSecretBackend()

	loaded, err := testStore(path, vault, &fakePasswordPrompter{}).LoadLogins()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Logins[0].Token != legacyToken {
		t.Fatal("migrated token was not returned")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(legacyToken)) {
		t.Fatal("legacy plaintext remained after migration")
	}
	if vault.values["account-one"] != legacyToken {
		t.Fatal("legacy token was not migrated to credential vault")
	}
}

func TestLegacyMigrationUsesEncryptedFallbackWhenVaultIsUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	legacyToken := "legacy-fallback-token"
	writeLegacyStore(t, path, []legacyLogin{
		{Email: "one@example.com", ID: "account-one", Token: legacyToken},
	})
	vault := newFakeSecretBackend()
	vault.failProbe = true
	store := testStore(path, vault, &fakePasswordPrompter{newValue: []byte(testPassword)})

	loaded, err := store.LoadLogins()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Backend != backendEncrypted || loaded.Logins[0].Token != legacyToken {
		t.Fatalf("unexpected migrated store: %#v", loaded)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(legacyToken)) {
		t.Fatal("legacy plaintext remained after encrypted migration")
	}
}

func TestLegacyMigrationDeclinedLeavesOriginalBytes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	original := writeLegacyStore(t, path, []legacyLogin{
		{Email: "one@example.com", ID: "account-one", Token: "legacy-token"},
	})
	vault := newFakeSecretBackend()
	store := testStore(path, vault, &fakePasswordPrompter{})
	store.prompts.confirmMigration = func() bool { return false }

	if _, err := store.LoadLogins(); !errors.Is(err, ErrMigrationDeclined) {
		t.Fatalf("got error %v, want ErrMigrationDeclined", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("declined migration changed the legacy store")
	}
	if len(vault.values) != 0 {
		t.Fatal("declined migration wrote credentials")
	}
}

func TestLegacyMigrationFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	original := writeLegacyStore(t, path, []legacyLogin{
		{Email: "one@example.com", ID: "account-one", Token: "token-one"},
		{Email: "two@example.com", ID: "account-two", Token: "token-two"},
	})
	vault := newFakeSecretBackend()
	vault.values["account-one"] = "previous-vault-token"
	vault.failSet["account-two"] = errors.New("injected failure")

	if _, err := testStore(path, vault, &fakePasswordPrompter{}).LoadLogins(); err == nil {
		t.Fatal("expected migration failure")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, original) {
		t.Fatal("failed migration changed the legacy store")
	}
	if vault.values["account-one"] != "previous-vault-token" {
		t.Fatal("failed migration did not restore the previous credential")
	}
}

func TestRecordedKeyringFailureDoesNotFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	vault := newFakeSecretBackend()
	creator := testStore(path, vault, &fakePasswordPrompter{})
	if err := creator.SaveLogin(CfLogin{
		Email: "one@example.com",
		ID:    "account-one",
		Token: "secret-token",
	}); err != nil {
		t.Fatal(err)
	}
	vault.failGet["account-one"] = errors.New("vault unavailable")

	_, err := testStore(path, vault, &fakePasswordPrompter{
		newValue: []byte(testPassword),
	}).LoadLogins()
	if err == nil || !strings.Contains(err.Error(), "credential vault") {
		t.Fatalf("expected credential-vault error, got %v", err)
	}
}

func TestLoadLoginsRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := testStore(path, newFakeSecretBackend(), &fakePasswordPrompter{}).LoadLogins(); err == nil {
		t.Fatal("expected malformed JSON to return an error")
	}
}

func base64Encode(value []byte) string {
	return base64.StdEncoding.EncodeToString(value)
}

func base64Decode(value string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(value)
}
