package main

import (
	"context"
	"os"

	"github.com/soha/soha/internal/cli/sohacli"
)

func main() {
	os.Exit(sohacli.Run(context.Background(), os.Args[1:], sohacli.Runtime{}))
}
