package main

import (
	"os"

	"github.com/sleepysoong/sleepyrouter/internal/cli"
)

func main() {
	os.Args[0] = "sleepyrouter"
	cli.Main()
}
