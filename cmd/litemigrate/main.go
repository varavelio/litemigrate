package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/varavelio/litemigrate/internal/app"
)

// main is the application entry point.
func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// run executes the CLI against the provided process-like dependencies.
func run(args []string, stdout, stderr io.Writer) error {
	application := app.New(stdout, stderr)
	return application.Run(context.Background(), args)
}
