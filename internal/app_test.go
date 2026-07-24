package internal

import (
	"bytes"
	"strings"
	"testing"
)

func TestPromptLoginActions(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantAction LoginAction
		wantIndex  int
	}{
		{name: "default", input: "\n", wantAction: LoginSelect, wantIndex: -1},
		{name: "select", input: "2\n", wantAction: LoginSelect, wantIndex: 1},
		{name: "add", input: "0\n", wantAction: LoginAdd},
		{name: "remove", input: "3\n", wantAction: LoginRemove},
		{name: "retry invalid", input: "-1\n1\n", wantAction: LoginSelect, wantIndex: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := newLogger(&output)
			got := promptLogin(logger, strings.NewReader(tt.input), &output, 2)
			if got.Action != tt.wantAction || got.Index != tt.wantIndex {
				t.Fatalf("got %#v, want action %v index %d", got, tt.wantAction, tt.wantIndex)
			}
		})
	}
}

func TestRemovalPrompts(t *testing.T) {
	logins := []CfLogin{
		{Email: "one@example.com"},
		{Email: "two@example.com"},
	}
	var output bytes.Buffer
	logger := newLogger(&output)

	index, ok := promptLoginRemoval(logger, strings.NewReader("bad\n2\n"), &output, logins)
	if !ok || index != 1 {
		t.Fatalf("got index %d, ok %v; want index 1", index, ok)
	}
	if confirmTokenRemoval(logger, strings.NewReader("n\n"), &output, logins[index].Email) {
		t.Fatal("expected removal confirmation to be declined")
	}
	if !promptAddAccount(logger, strings.NewReader("y\n"), &output) {
		t.Fatal("expected add-account prompt to be accepted")
	}
}
