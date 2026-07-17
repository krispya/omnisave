// Package locator discovers RetroArch installations without interpreting their saves.
package locator

import (
	"context"
	"os"
	"path/filepath"
)

// Candidate describes an installation found by one locator.
type Candidate struct {
	Source     string
	Root       string
	ConfigPath string
}

// Locator finds RetroArch installation candidates.
type Locator interface {
	Locate(context.Context) ([]Candidate, error)
}

func existingCandidates(ctx context.Context, source string, configPaths []string) ([]Candidate, error) {
	var candidates []Candidate
	for _, configPath := range configPaths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if configPath == "" {
			continue
		}
		info, err := os.Stat(configPath)
		if err == nil && info.Mode().IsRegular() {
			candidates = append(candidates, Candidate{
				Source:     source,
				Root:       filepath.Dir(configPath),
				ConfigPath: configPath,
			})
		} else if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	return candidates, nil
}
