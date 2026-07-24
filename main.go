// Command droid-switcher manages multiple local Factory Droid logins.
package main

import (
	"droid-switcher/internal/switcher"
	"fmt"
	"os"
)

func main() {
	if err := switcher.Run(os.Args, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
