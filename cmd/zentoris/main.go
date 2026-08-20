package main

import (
	"context"
	"os"

	"github.com/zentoris-labs/ztr-cli/internal/commands"
)

func main() {
	if err := commands.NewRootCmd().ExecuteContext(context.Background()); err != nil {
		// cobra already printed the error; exit non-zero so CI fails.
		os.Exit(1)
	}
}
