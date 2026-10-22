package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/botenvouwer/gitmaster/internal/config"
	"github.com/botenvouwer/gitmaster/internal/update"
)

// checkEvery is the shortest time between two update checks at start.
const checkEvery = time.Hour

// runUpdate implements `gitmaster update` and returns the exit code.
func runUpdate(current string, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fs.Bool("check", false, "only report whether a newer release exists")
	auto := fs.String("auto", "", "turn the update check at start \"on\" or \"off\"")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *auto != "" {
		return setAutoUpdate(*auto)
	}
	if _, ok := update.Parse(current); !ok {
		fmt.Fprintf(os.Stderr, "gitmaster %s is a development build; install a release to use update\n", current)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), update.InstallTimeout)
	defer cancel()
	rel, err := update.Latest(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		return 1
	}
	if !update.Newer(current, rel.Tag) {
		fmt.Printf("gitmaster %s is up to date\n", current)
		return 0
	}
	if *check {
		fmt.Printf("gitmaster %s is available (you have %s): run gitmaster update\n", rel.Tag, current)
		return 0
	}
	exe, err := update.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		return 1
	}
	if err := update.Install(ctx, rel, exe); err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		return 1
	}
	fmt.Printf("updated gitmaster %s -> %s (%s)\n", current, rel.Tag, exe)
	return 0
}

func setAutoUpdate(value string) int {
	var on bool
	switch value {
	case "on":
		on = true
	case "off":
	default:
		fmt.Fprintln(os.Stderr, `update: --auto takes "on" or "off"`)
		return 2
	}
	g, err := config.LoadGlobal()
	if err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		return 1
	}
	g.Settings.AutoUpdate = on
	if err := g.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "update:", err)
		return 1
	}
	fmt.Println("auto update is", value)
	return 0
}

// autoUpdate runs before the UI opens when settings.autoUpdate is on: it asks
// for the newest release (at most once an hour, 3 s at most), installs it and
// restarts into it. Whatever goes wrong, gitmaster still starts.
func autoUpdate(current string) {
	if os.Getenv(update.NoUpdateEnv) != "" {
		return
	}
	if _, ok := update.Parse(current); !ok {
		return // a development build never replaces itself
	}
	g, err := config.LoadGlobal()
	if err != nil || !g.Settings.AutoUpdate || time.Since(g.Update.LastCheck) < checkEvery {
		return
	}
	g.Update.LastCheck = time.Now() // also on failure, so being offline costs 3 s an hour, not at every start
	_ = g.Save()

	ctx, cancel := context.WithTimeout(context.Background(), update.CheckTimeout)
	rel, err := update.Latest(ctx)
	cancel()
	if err != nil || !update.Newer(current, rel.Tag) {
		return
	}
	fmt.Fprintf(os.Stderr, "gitmaster %s -> %s: updating...\n", current, rel.Tag)
	ctx, cancel = context.WithTimeout(context.Background(), update.InstallTimeout)
	defer cancel()
	exe, err := update.Executable()
	if err == nil {
		err = update.Install(ctx, rel, exe)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "auto update failed:", err)
		return
	}
	if err := update.Restart(exe); err != nil {
		fmt.Fprintln(os.Stderr, "updated; start gitmaster again to use", rel.Tag)
	}
}
