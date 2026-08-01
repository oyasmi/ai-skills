package main

import (
	"context"
	"os"

	"github.com/oyasmi/ai-skills/tools/agentmux/internal/app"
)

var version = "dev"
var buildTime = "unknown"

func main() {
	ctx := context.Background()
	app.Version = version
	app.BuildTime = buildTime
	code := app.Run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	if code != 0 {
		os.Exit(code)
	}
}
