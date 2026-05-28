
package main

import (
	"os"

	"github.com/aum12/todoist-cli/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
