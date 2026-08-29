package main

import (
	"fmt"
	"os"

	"github.com/alexpermiakov/platform-cli/internal/cli"
)

func main() {
	if err := cli.NewRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
