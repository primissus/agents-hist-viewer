// cronctl installs or removes the chv index crontab entry.
// Invoked by Makefile targets install-cron / uninstall-cron — not part of the chv CLI.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"claude-code-hist-viewer/internal/config"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "install":
		cmdInstall(os.Args[2:])
	case "uninstall":
		cmdUninstall(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: cronctl install|uninstall")
}

func cmdInstall(args []string) {
	fs := flag.NewFlagSet("install", flag.ExitOnError)
	schedule := fs.String("schedule", "0 */4 * * *", "cron schedule expression")
	chvPath := fs.String("chv", "", "path to chv binary")
	fs.Parse(args)

	bin := *chvPath
	if bin == "" {
		fmt.Fprintln(os.Stderr, "cronctl install: --chv is required")
		os.Exit(1)
	}
	bin, err := filepath.Abs(bin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cronctl install: %v\n", err)
		os.Exit(1)
	}

	if err := config.InstallCronJob(bin, *schedule); err != nil {
		fmt.Fprintf(os.Stderr, "install-cron: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Installed cron job (%s)\n", *schedule)
	fmt.Printf("  %s\n", config.CronEntry(bin, *schedule, config.IndexLogPath()))
}

func cmdUninstall(args []string) {
	fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
	fs.Parse(args)

	if err := config.UninstallCronJob(); err != nil {
		fmt.Fprintf(os.Stderr, "uninstall-cron: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Removed chv cron job")
}
