// Command qtogo installs and manages Qt SDKs from the official online
// repositories.
package main

import (
	"os"

	"github.com/nekrozis/qtogo/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
