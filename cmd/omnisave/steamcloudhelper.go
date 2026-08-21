package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/krisbaumgartner/omnisave/internal/client/target/steam/steamworks"
)

// steamCloudHelperCommand is the hidden subcommand the client re-executes
// itself with to reconcile one game's Steam Cloud registry. A Steamworks
// library can only ever speak as one game per process, so each
// reconciliation gets a process of its own; the request arrives on stdin
// and the result leaves on stdout, both JSON.
const steamCloudHelperCommand = steamworks.HelperCommand

func runSteamCloudHelper(input io.Reader, output io.Writer) error {
	var request steamworks.Request
	if err := json.NewDecoder(input).Decode(&request); err != nil {
		return fmt.Errorf("read reconciliation request: %w", err)
	}
	result, err := steamworks.Run(request)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(result)
}
