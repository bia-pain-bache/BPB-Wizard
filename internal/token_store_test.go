package internal

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteLogin(t *testing.T) {
	tests := []struct {
		name       string
		active     string
		remove     string
		wantActive string
		wantLogins int
	}{
		{
			name:       "non-active account",
			active:     "one@example.com",
			remove:     "two@example.com",
			wantActive: "one@example.com",
			wantLogins: 1,
		},
		{
			name:       "active account",
			active:     "one@example.com",
			remove:     "one@example.com",
			wantActive: "two@example.com",
			wantLogins: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "store.json")
			store := newTokenStore(path)
			if err := store.createStore(cfLoginStore{
				ActiveEmail: tt.active,
				Logins: []CfLogin{
					{Email: "one@example.com", ID: "one", Token: "token-one"},
					{Email: "two@example.com", ID: "two", Token: "token-two"},
				},
			}); err != nil {
				t.Fatal(err)
			}

			got, err := store.DeleteLogin(tt.remove)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Logins) != tt.wantLogins {
				t.Fatalf("got %d logins, want %d", len(got.Logins), tt.wantLogins)
			}
			if got.ActiveEmail != tt.wantActive {
				t.Fatalf("got active email %q, want %q", got.ActiveEmail, tt.wantActive)
			}
		})
	}
}

func TestDeleteFinalLoginRemovesStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	store := newTokenStore(path)
	if err := store.createStore(cfLoginStore{
		ActiveEmail: "one@example.com",
		Logins: []CfLogin{
			{Email: "one@example.com", ID: "one", Token: "token-one"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := store.DeleteLogin("one@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Logins) != 0 || got.ActiveEmail != "" {
		t.Fatalf("expected an empty store, got %#v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected store file to be removed, stat error: %v", err)
	}
}

func TestLoadLoginsRejectsMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := newTokenStore(path).LoadLogins(); err == nil {
		t.Fatal("expected malformed JSON to return an error")
	}
}
