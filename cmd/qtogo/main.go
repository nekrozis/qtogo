// Command qtogo installs and manages Qt SDKs from the official online
// repositories.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/nekrozis/qtogo/internal/cli"
	"github.com/nekrozis/qtogo/internal/exitcode"
	"github.com/nekrozis/qtogo/internal/service"
	"github.com/nekrozis/qtogo/internal/transport"
)

func main() {
	os.Exit(realMain())
}

// realMain runs the command line and returns the status to leave with, so the
// deferred stop below runs before the process ends.
func realMain() int {
	// An interrupt cancels the request that is running.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return cli.Main(ctx, os.Args[1:], os.Stdout, os.Stderr, newService())
}

// newService builds the repository layers the commands run on.
func newService() cli.Services {
	client, err := transport.New(transport.Config{})
	if err != nil {
		// The default configuration cannot fail; if it ever does, say so rather
		// than start a front end that cannot reach a repository.
		fmt.Fprintf(os.Stderr, "qtogo: %v\n", err)
		os.Exit(exitcode.Config)
	}
	return service.New(client)
}
