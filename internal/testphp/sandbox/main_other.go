//go:build !linux || !amd64

package main

import (
	"fmt"
	"os"
)

func main() { fmt.Fprintln(os.Stderr, "PHP_SANDBOX_UNAVAILABLE: linux/amd64 only"); os.Exit(125) }
