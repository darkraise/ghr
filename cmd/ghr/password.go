package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// stdinIsTerminal and readPassword are replaced in tests.
var (
	stdinIsTerminal = func() bool { return fdIsTerminal(os.Stdin.Fd()) }
	readPassword    = func() ([]byte, error) { return term.ReadPassword(int(os.Stdin.Fd())) }
)

// maxPipedPassword bounds a password read from a pipe; the daemon allows 1024 bytes.
const maxPipedPassword = 4 << 10

// readNewPassword asks twice without echo on a terminal, or reads one
// password from a pipe. The daemon checks the length.
func readNewPassword(ctx context.Context, stdin io.Reader, prompts io.Writer) (string, error) {
	if !stdinIsTerminal() {
		data, err := io.ReadAll(io.LimitReader(stdin, maxPipedPassword))
		if err != nil {
			return "", err
		}
		s := string(data)
		// Only the line ending goes: other whitespace may be part of the password.
		if strings.HasSuffix(s, "\r\n") {
			return s[:len(s)-2], nil
		}
		return strings.TrimSuffix(s, "\n"), nil
	}
	first, err := askPassword(ctx, prompts, "New web password: ")
	if err != nil {
		return "", err
	}
	second, err := askPassword(ctx, prompts, "Repeat: ")
	if err != nil {
		return "", err
	}
	if first != second {
		return "", errors.New("passwords do not match")
	}
	return first, nil
}

// askPassword reads one line without echo. Ctrl-C only cancels ctx, which
// the blocked read does not see, so ctx is checked once the read returns.
func askPassword(ctx context.Context, prompts io.Writer, prompt string) (string, error) {
	fmt.Fprint(prompts, prompt)
	b, err := readPassword()
	fmt.Fprintln(prompts)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(b), nil
}
