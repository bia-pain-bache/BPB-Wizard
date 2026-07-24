package internal

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestPasswordPromptRequiresInteractiveTerminal(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "password-input")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()

	prompter := newTerminalPasswordPrompter(input, io.Discard)
	_, err = prompter.ExistingPassword()
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("expected interactive-terminal error, got %v", err)
	}
}
