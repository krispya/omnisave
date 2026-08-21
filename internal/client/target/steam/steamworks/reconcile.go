package steamworks

import (
	"fmt"
	"os"
)

// HelperCommand names the client's hidden subcommand that runs one
// reconciliation in a process of its own. The name is shared here so the
// command and its caller cannot drift apart.
const HelperCommand = "steam-cloud-helper"

// Request asks for one game's registry to be reconciled with placed files.
// It is the wire format between the client and the helper process that
// holds the Steamworks connection.
type Request struct {
	// Library is the game's own Steamworks library.
	Library string `json:"library"`
	// AppID is the Steam application the placement belongs to.
	AppID string `json:"app_id"`
	// Files are the placed files' absolute native paths.
	Files []string `json:"files"`
	// DryRun computes and reports the plan without writing anything.
	DryRun bool `json:"dry_run,omitempty"`
}

// Failure is one registry write that did not take.
type Failure struct {
	Name  string `json:"name"`
	Cause string `json:"cause"`
}

// Result reports what became of a reconciliation. Every list speaks in
// registry names, so a report reads in the store's vocabulary.
type Result struct {
	// Skipped is why nothing was attempted; empty when the plan ran.
	Skipped string `json:"skipped,omitempty"`
	// Anchor is the local directory the registry proved itself relative to.
	Anchor string `json:"anchor,omitempty"`
	// Written are entries created or refreshed (planned ones on a dry run).
	Written []string `json:"written,omitempty"`
	// Unchanged are entries that already held their file's exact bytes.
	Unchanged []string `json:"unchanged,omitempty"`
	// Ineligible are placed files the registry gives no precedent for.
	Ineligible []string `json:"ineligible,omitempty"`
	// Extras are registry entries the placement carries no file for,
	// left in place and reported (FDR-005).
	Extras []string `json:"extras,omitempty"`
	// Outside counts placed files that lie outside the anchor.
	Outside int `json:"outside,omitempty"`
	// Failed are writes Steam refused.
	Failed []Failure `json:"failed,omitempty"`
}

// registry is the store connection a reconciliation drives. *Client
// implements it; tests substitute their own.
type registry interface {
	Registry() []RegistryFile
	Holds(name string, content []byte) bool
	WriteFile(name string, content []byte) error
}

// Reconcile makes the store's registry match the placed files, to the
// extent the registry's own evidence allows (see PlanReconciliation). It
// never deletes: entries the placement does not carry are reported as
// extras, since whether a restore must remove them is an open measurement.
func Reconcile(store registry, request Request) Result {
	plan, anchored := PlanReconciliation(store.Registry(), request.Files)
	if !anchored {
		return Result{Skipped: "the registry's names prove no anchor among the placed files"}
	}
	result := Result{
		Anchor:     plan.Anchor,
		Ineligible: plan.Ineligible,
		Extras:     plan.Extras,
		Outside:    len(plan.Outside),
	}
	for _, write := range plan.Writes {
		content, err := os.ReadFile(write.Path)
		if err != nil {
			result.Failed = append(result.Failed, Failure{Name: write.Name, Cause: err.Error()})
			continue
		}
		if write.Listed && store.Holds(write.Name, content) {
			result.Unchanged = append(result.Unchanged, write.Name)
			continue
		}
		if request.DryRun {
			result.Written = append(result.Written, write.Name)
			continue
		}
		if err := store.WriteFile(write.Name, content); err != nil {
			result.Failed = append(result.Failed, Failure{Name: write.Name, Cause: err.Error()})
			continue
		}
		result.Written = append(result.Written, write.Name)
	}
	return result
}

// Run connects to Steam as the requested game, reconciles, and disconnects.
// The connection is held only as long as the writes take, since it presents
// the account as playing the game.
func Run(request Request) (Result, error) {
	if request.Library == "" || request.AppID == "" {
		return Result{}, fmt.Errorf("reconciliation needs a library and an app id")
	}
	client, err := Connect(request.Library, request.AppID)
	if err != nil {
		return Result{}, err
	}
	defer client.Close()
	return Reconcile(client, request), nil
}
