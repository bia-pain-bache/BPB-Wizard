package internal

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
)

type terminalPasswordPrompter struct {
	input  *os.File
	output io.Writer
}

func newTerminalPasswordPrompter(input *os.File, output io.Writer) passwordPrompter {
	return &terminalPasswordPrompter{input: input, output: output}
}

func (p *terminalPasswordPrompter) ExistingPassword() ([]byte, error) {
	return p.readPassword("Master password: ")
}

func (p *terminalPasswordPrompter) NewPassword() ([]byte, error) {
	for {
		password, err := p.readPassword("Create a master password: ")
		if err != nil {
			return nil, err
		}
		if len(password) < 8 {
			clearBytes(password)
			fmt.Fprintln(p.output, "Master password must contain at least 8 characters.")
			continue
		}
		confirmation, err := p.readPassword("Confirm master password: ")
		if err != nil {
			clearBytes(password)
			return nil, err
		}
		matches := bytes.Equal(password, confirmation)
		clearBytes(confirmation)
		if matches {
			return password, nil
		}
		clearBytes(password)
		fmt.Fprintln(p.output, "Master passwords do not match, try again.")
	}
}

func (p *terminalPasswordPrompter) readPassword(prompt string) ([]byte, error) {
	fd := int(p.input.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("protected token storage requires an interactive terminal")
	}
	fmt.Fprint(p.output, prompt)
	password, err := term.ReadPassword(fd)
	fmt.Fprintln(p.output)
	if err != nil {
		return nil, fmt.Errorf("read master password: %w", err)
	}
	return password, nil
}
