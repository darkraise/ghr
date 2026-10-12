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

// errInterrupted reports Ctrl-C or Ctrl-D at a prompt. In raw mode the
// terminal hands both to the reader as bytes and raises no signal.
var errInterrupted = errors.New("interrupted")

// stdinIsTerminal, hideInput and readPassword are replaced in tests.
var (
	stdinIsTerminal = func() bool { return fdIsTerminal(os.Stdin.Fd()) }
	hideInput       = func() (func(), error) { return hideTerminal(os.Stdin) }
	readPassword    = func() ([]byte, error) { return readHidden(os.Stdin) }
)

// hideTerminal puts f's terminal into raw mode, which echoes nothing, and
// returns the function that puts back the state it found.
func hideTerminal(f *os.File) (func(), error) {
	fd := int(f.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() { term.Restore(fd, state) }, nil
}

// readHidden reads one line typed into a terminal that is in raw mode. It
// changes no terminal mode itself: askPassword must stay the only code that
// does, or a cancelled prompt could put the terminal back too early.
func readHidden(in io.Reader) ([]byte, error) {
	rw := struct {
		io.Reader
		io.Writer
	}{in, io.Discard}
	line, err := term.NewTerminal(rw, "").ReadPassword("")
	if errors.Is(err, io.EOF) {
		return nil, errInterrupted
	}
	return []byte(line), err
}

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

// askPassword reads one line without echo. It alone changes the terminal's
// mode, on this goroutine: it hides the input before the read starts and puts
// the terminal back once the read or ctx has ended, whichever is first. The
// read blocks and does not see ctx, so it runs on its own goroutine and, when
// ctx ends first, is left behind for the exiting process.
func askPassword(ctx context.Context, prompts io.Writer, prompt string) (string, error) {
	restore, err := hideInput()
	if err != nil {
		return "", err
	}
	fmt.Fprint(prompts, prompt)
	type answer struct {
		b   []byte
		err error
	}
	// The goroutine calls the function read here: a prompt that was
	// cancelled must not read the variable later.
	read, done := readPassword, make(chan answer, 1)
	go func() {
		b, err := read()
		done <- answer{b, err}
	}()
	var a answer
	select {
	case a = <-done:
	case <-ctx.Done():
	}
	restore()
	fmt.Fprintln(prompts)
	if a.err != nil {
		return "", a.err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return string(a.b), nil
}
