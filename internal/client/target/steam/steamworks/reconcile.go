package steamworks

import (
	"fmt"
	"os"
	"sort"
	"strings"
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
	// Removed are absolute native paths the placement removed from the
	// save folder. Only content a committed revision holds is ever removed
	// by a placing flow, which is what entitles the reconciliation to also
	// delete the matching registry entries (FDR-005, decision 13).
	Removed []string `json:"removed,omitempty"`
	// Before pins cloud mutations to bytes preserved before the placement.
	Before map[string]string `json:"before,omitempty"`
	// DryRun computes and reports the plan without writing anything.
	DryRun bool `json:"dry_run,omitempty"`
}

// Failure is one registry write or deletion that did not take.
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
	// Deleted are entries removed from the store because the placement
	// removed their local files (planned ones on a dry run).
	Deleted []string `json:"deleted,omitempty"`
	// Extras are registry entries the placement carries no file for and no
	// removal vouches for, left in place and reported (FDR-005).
	Extras []string `json:"extras,omitempty"`
	// Outside counts placed files that lie outside the anchor.
	Outside int `json:"outside,omitempty"`
	// Failed are writes or deletions that could not complete.
	Failed []Failure `json:"failed,omitempty"`
}

// registry is the store connection a reconciliation drives. *Client
// implements it; tests substitute their own.
type registry interface {
	Registry() []RegistryFile
	Holds(name string, content []byte) bool
	Digest(name string) (string, error)
	Exists(name string) bool
	WriteFile(name string, content []byte) error
	DeleteFile(name string) error
}

// Reconcile makes the store's registry match the placed files, to the
// extent the registry's own evidence allows (see PlanReconciliation).
// Deletion is bounded by the same evidence as writes: only entries whose
// local files the placement itself removed are deleted — measured (FDR-005,
// 2026-08-25): the store resurrects an undeleted extra at the game's next
// launch, and the game may act on it. Entries no removal vouches for are
// reported as extras and left.
func Reconcile(store registry, request Request) Result {
	plan, anchored := PlanReconciliation(store.Registry(), request.Files, request.Removed)
	if !anchored {
		return Result{Skipped: "the registry's names prove no anchor among the placed files"}
	}
	result := Result{
		Anchor:     plan.Anchor,
		Ineligible: plan.Ineligible,
		Extras:     plan.Extras,
		Outside:    len(plan.Outside),
	}
	// Read and validate the complete plan before any mutation. Unknown cloud
	// content belongs to another writer, not to this restore.
	contents := make(map[string][]byte, len(plan.Writes))
	unchanged := make(map[string]bool)
	checkBefore := func(name, localPath string) bool {
		expected := request.Before[localPath]
		digest, err := store.Digest(name)
		if err != nil || expected == "" || !strings.EqualFold(digest, expected) {
			result.Failed = append(result.Failed, Failure{Name: name, Cause: "cloud content differs from the preserved baseline"})
			return false
		}
		return true
	}
	for _, write := range plan.Writes {
		content, err := os.ReadFile(write.Path)
		if err != nil {
			result.Failed = append(result.Failed, Failure{Name: write.Name, Cause: "cloud operation or local read failed"})
			continue
		}
		if write.Listed && store.Holds(write.Name, content) {
			result.Unchanged = append(result.Unchanged, write.Name)
			unchanged[write.Name] = true
			continue
		}
		contents[write.Name] = content
		if write.Listed {
			checkBefore(write.Name, write.Path)
		}
	}
	for _, deletion := range plan.Deletes {
		checkBefore(deletion.Name, deletion.Path)
	}
	if len(result.Failed) > 0 {
		for _, deletion := range plan.Deletes {
			result.Extras = append(result.Extras, deletion.Name)
		}
		sort.Strings(result.Extras)
		return result
	}
	for _, write := range plan.Writes {
		if unchanged[write.Name] {
			continue
		}
		if request.DryRun {
			result.Written = append(result.Written, write.Name)
			continue
		}
		if store.Exists(write.Name) && !store.Holds(write.Name, contents[write.Name]) && !checkBefore(write.Name, write.Path) {
			continue
		}
		if err := store.WriteFile(write.Name, contents[write.Name]); err != nil {
			result.Failed = append(result.Failed, Failure{Name: write.Name, Cause: "cloud write failed"})
			continue
		}
		result.Written = append(result.Written, write.Name)
	}
	// Never retire cloud state until every planned replacement succeeded.
	// Deferred deletions remain visible and can be retried with the request.
	if len(result.Failed) > 0 {
		for _, deletion := range plan.Deletes {
			result.Extras = append(result.Extras, deletion.Name)
		}
		sort.Strings(result.Extras)
		return result
	}
	for _, deletion := range plan.Deletes {
		name, localPath := deletion.Name, deletion.Path
		if request.DryRun {
			result.Deleted = append(result.Deleted, name)
			continue
		}
		if _, err := os.Lstat(localPath); !os.IsNotExist(err) {
			result.Failed = append(result.Failed, Failure{Name: name, Cause: "removed local file reappeared or cannot be checked"})
			continue
		}
		if store.Exists(name) && !checkBefore(name, localPath) {
			continue
		}
		if err := store.DeleteFile(name); err != nil {
			result.Failed = append(result.Failed, Failure{Name: name, Cause: "cloud operation or local read failed"})
			continue
		}
		result.Deleted = append(result.Deleted, name)
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
