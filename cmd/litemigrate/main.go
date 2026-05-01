package main

import (
	"fmt"
	"os"
)

// main is the application entry point, this just calls the run function and handles any
// errors that it returns by printing them and exiting with a non-zero status code.
func main() {
	if err := run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// run is the main function that contains the logic of the application. It returns an error
// if something goes wrong, which is handled by the main function.
func run() error {
	return nil
}
