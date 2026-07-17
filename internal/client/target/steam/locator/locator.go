// Package locator discovers Steam client installations.
package locator

import "context"

// Candidate describes a Steam installation found by a locator.
type Candidate struct {
	Source string
	Root   string
}

// Locator finds Steam installation candidates.
type Locator interface {
	Locate(context.Context) ([]Candidate, error)
}
