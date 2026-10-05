//go:build !unix

package system

import "os/exec"

// killGroup leaves cmd as it is: ghr runs on Linux, and no test elsewhere
// starts a script.
func killGroup(*exec.Cmd) {}
