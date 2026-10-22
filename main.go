// gitmaster is a k9s-style TUI for browsing the git repos in a directory.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/botenvouwer/gitmaster/internal/ui"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

// currentVersion is the build's version; a binary made by `go install
// ...@vX.Y.Z` carries it in its build info instead of in ldflags.
func currentVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: gitmaster [dir]\n       gitmaster update [--check] [--auto on|off]\n\nBrowse the git repos in dir (default: current directory).\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	ver := currentVersion()
	if *showVersion {
		fmt.Println("gitmaster", ver)
		return
	}
	if flag.Arg(0) == "update" {
		os.Exit(runUpdate(ver, flag.Args()[1:]))
	}
	autoUpdate(ver)
	dir := "."
	if flag.NArg() > 0 {
		dir = flag.Arg(0)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "not a directory: %s\n", abs)
		os.Exit(1)
	}
	if err := ui.New(abs, ver).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
