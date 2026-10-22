//go:build windows

package update

import "fmt"

// Restart is not supported on windows.
func Restart(exe string) error { return fmt.Errorf("restart is not supported on windows") }
