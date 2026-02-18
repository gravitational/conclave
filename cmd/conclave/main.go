package main

import (
	"fmt"
	"os"

	"github.com/rob-picard-teleport/conclave/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "\033[31m✗\033[0m %s\n", err)
		os.Exit(1)
	}
}
