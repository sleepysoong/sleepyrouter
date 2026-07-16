package main

import (
	"github.com/sleepysoong/sleepyrouter"
	"os"
)

func main() {
	os.Args[0] = "sleepyrouter"
	sleepyrouter.Main()
}
