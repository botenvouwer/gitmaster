//go:build !windows

package update

import (
	"os"
	"syscall"
)

// Restart replaces this process with the (updated) binary at exe, keeping the
// arguments. On success it does not return.
func Restart(exe string) error {
	return syscall.Exec(exe, os.Args, append(os.Environ(), NoUpdateEnv+"=1"))
}
