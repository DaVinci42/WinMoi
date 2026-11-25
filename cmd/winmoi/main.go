package main

import (
	"fmt"
	"os"

	"github.com/davinci42/WinMoi/internal/cli"
	"github.com/davinci42/WinMoi/internal/exitcode"
)

func main() {
	cmd := cli.NewRootCmd()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "winmoi: %v\n", err)
		os.Exit(exitcode.FromError(err))
	}
}
