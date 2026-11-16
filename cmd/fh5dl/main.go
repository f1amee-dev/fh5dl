package main

import (
	"context"
	"os"
	"os/signal"
	"runtime/debug"

	"github.com/f1amee-dev/fh5dl/v2/internal/app"
)

var version, commit string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	v := version
	if v == "" {
		v = "devel"
		if info, ok := debug.ReadBuildInfo(); ok {
			if info.Main.Version != "" && info.Main.Version != "(devel)" {
				v = info.Main.Version
			}
			for _, s := range info.Settings {
				if s.Key == "vcs.revision" && commit == "" {
					commit = s.Value
				}
			}
		}
	}
	if commit != "" {
		v += " (" + commit + ")"
	}
	os.Exit(app.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr, v))
}
