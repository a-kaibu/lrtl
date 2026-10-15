package main

import (
	"os"

	"github.com/a-kaibu/lrtl/internal/lrtl"
)

func main() {
	os.Exit(lrtl.Main(os.Args[1:], os.Stdout, os.Stderr))
}
