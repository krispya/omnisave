// Package activity carries best-effort, request-scoped progress updates.
package activity

import "context"

type reporterKey struct{}

// Reporter receives a short description of the work currently in progress.
// Reporters must be safe to call from the goroutine doing the work.
type Reporter func(string)

// WithReporter attaches optional progress reporting to a request context.
func WithReporter(ctx context.Context, reporter Reporter) context.Context {
	if reporter == nil {
		return ctx
	}
	return context.WithValue(ctx, reporterKey{}, reporter)
}

// Report publishes a progress update when the request has a reporter.
func Report(ctx context.Context, message string) {
	reporter, _ := ctx.Value(reporterKey{}).(Reporter)
	if reporter != nil {
		reporter(message)
	}
}
