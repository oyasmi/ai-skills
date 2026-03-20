package main

import (
	"context"
	"os"

	"github.com/oyasmi/agentmux/internal/app"
)

var version = "dev"

func main() {
	ctx := context.Background()
	app.Version = version
	code := app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	if code != 0 {
		os.Exit(code)
	}
}
