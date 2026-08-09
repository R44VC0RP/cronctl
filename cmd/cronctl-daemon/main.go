package main

import (
	"os"

	"github.com/R44VC0RP/cronctl/internal/cronctl"
)

func main() {
	os.Exit(cronctl.Main([]string{"daemon", "run"}))
}
