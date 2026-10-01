// Command rivet links dotfile sets into their destinations and keeps them in
// sync.
package main

import (
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := newRootCmd(version).Execute(); err != nil {
		os.Exit(1)
	}
}
