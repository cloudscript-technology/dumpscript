// Command dumpscript is the CLI entry point — see internal/cli for the
// command tree and the design-pattern-based internals.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/cloudscript-technology/dumpscript/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.NewRoot().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "dumpscript:", err)
		os.Exit(cli.ExitCode(err))
	}
}
