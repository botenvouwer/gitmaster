// gitmaster is a k9s-style TUI for browsing the git repos in a directory.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/botenvouwer/gitmaster/internal/ui"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: gitmaster [dir]\n\nBrowse the git repos in dir (default: current directory).\n\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("gitmaster", version)
		return
	}
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
	if err := ui.New(abs, version).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
