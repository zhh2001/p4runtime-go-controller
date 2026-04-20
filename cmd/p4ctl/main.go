// Command p4ctl is the reference CLI for p4runtime-go-controller.
package main

import (
	"fmt"
	"os"

	"github.com/zhh2001/p4runtime-go-controller/cmd/p4ctl/cmd"
)

// version is populated at build time by goreleaser ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd.Version = fmt.Sprintf("p4ctl %s (%s, %s)", version, commit, date)
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
