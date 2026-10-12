package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/term"
)

// openPTY returns both ends of a new pseudo-terminal. The second is a real
// terminal as far as the prompt can tell; what is written to the first
// arrives there as typed.
func openPTY(t *testing.T) (master, tty *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals here: %v", err)
	}
	t.Cleanup(func() { master.Close() })
	var unlock int32
	var number uint32
	for _, call := range []struct {
		request uintptr
		arg     unsafe.Pointer
	}{{syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)}, {syscall.TIOCGPTN, unsafe.Pointer(&number)}} {
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, master.Fd(), call.request, uintptr(call.arg)); errno != 0 {
			t.Fatalf("pseudo-terminal ioctl %#x: %v", call.request, errno)
		}
	}
	tty, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tty.Close() })
	return master, tty
}

// The stubs of the other tests only model a terminal. Here the prompt runs
// its real mode changes and its real read against a pseudo-terminal, and the
// terminal must end in the state it was found in.
func TestPasswordPromptOnARealTerminal(t *testing.T) {
	for _, tc := range []struct {
		name  string
		typed string // typed once the input is hidden; "" cancels before the read starts
		want  string
		err   error
	}{
		{"a line is typed", "a long secret\r", "a long secret", nil},
		{"Ctrl-C is typed", "a long\x03", "", errInterrupted},
		{"cancelled before the read starts", "", "", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			master, tty := openPTY(t)
			stubPasswordInput(t, true)
			found, err := term.GetState(int(tty.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			hideInput = func() (func(), error) {
				restore, err := hideTerminal(tty)
				if err != nil {
					return nil, err
				}
				if tc.typed == "" {
					cancel()
				} else if _, err := master.WriteString(tc.typed); err != nil {
					t.Error(err)
				}
				return restore, nil
			}
			readDone := make(chan struct{})
			readPassword = func() ([]byte, error) {
				defer close(readDone)
				return readHidden(tty)
			}
			var prompts bytes.Buffer
			got, err := askPassword(ctx, &prompts, "New web password: ")
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Errorf("password %q err %v, want %q and %v", got, err, tc.want, tc.err)
			}
			if tc.typed == "" {
				// The read left behind ends with the next line typed. It
				// must leave the terminal as the prompt put it back.
				if _, err := master.WriteString("typed too late\r"); err != nil {
					t.Fatal(err)
				}
			}
			<-readDone
			left, err := term.GetState(int(tty.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(left, found) {
				t.Fatalf("the terminal was left in another state than it was found in:\n%+v\n%+v", left, found)
			}
		})
	}
}
