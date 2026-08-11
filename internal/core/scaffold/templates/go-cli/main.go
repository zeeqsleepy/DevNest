package main

import (
	"flag"
	"fmt"
	"os"
)

// A starter command-line program. Run it with `go run .`.
func main() {
	name := flag.String("name", "world", "who to greet")
	flag.Parse()

	if *name == "" {
		fmt.Fprintln(os.Stderr, "the name must not be empty")
		os.Exit(2)
	}
	fmt.Printf("hello, %s\n", *name)
}
