package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/krisbaumgartner/omnisave/internal/client"
	"github.com/krisbaumgartner/omnisave/internal/client/target/retroarch"
	"github.com/krisbaumgartner/omnisave/internal/client/target/steam"
	"github.com/krisbaumgartner/omnisave/internal/client/tui"
)

func main() {
	var verbose bool
	flag.BoolVar(&verbose, "verbose", false, "show games and individual save files")
	flag.BoolVar(&verbose, "v", false, "show games and individual save files")
	flag.Parse()

	scanner := client.NewScanner(nil, retroarch.NewDefault(), steam.NewDefault())
	if err := tui.Run(context.Background(), scanner, verbose); err != nil {
		fmt.Fprintf(os.Stderr, "scan saves: %v\n", err)
		os.Exit(1)
	}
}
