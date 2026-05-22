// Package main is the CLI entry point for the Gresbase server.
package main

import (
	"os"

	"github.com/gresbase/gresbase/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
