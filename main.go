package main

import (
	"fmt"
	"os"

	"droid-switcher/internal/switcher"
)

func main() {
	if err := switcher.Run(os.Args, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
