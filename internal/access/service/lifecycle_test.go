package service_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/krisbaumgartner/omnisave/internal/access"
	accessservice "github.com/krisbaumgartner/omnisave/internal/access/service"
)

func TestAuthenticationFinishesCredentialUseBeforeReturning(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repository := &blockedCredentialUse{started: make(chan struct{}), release: make(chan struct{})}
		service := accessservice.New(repository, ownerToken)
		finished := make(chan struct{})
		var authenticateErr error
		go func() {
			_, authenticateErr = service.Authenticate(context.Background(), "issued-token")
			close(finished)
		}()

		<-repository.started
		synctest.Wait()
		select {
		case <-finished:
			t.Error("authentication returned with a database write still running")
		default:
		}
		close(repository.release)
		<-finished
		if authenticateErr != nil {
			t.Fatalf("best-effort credential use prevented authentication: %v", authenticateErr)
		}
	})
}

type blockedCredentialUse struct {
	access.Repository
	started chan struct{}
	release chan struct{}
}

func (r *blockedCredentialUse) FindCredentialByTokenHash(context.Context, string) (*access.Credential, error) {
	return &access.Credential{ID: "device-credential", Kind: access.KindDevice}, nil
}

func (r *blockedCredentialUse) TouchCredential(context.Context, string, time.Time) error {
	close(r.started)
	<-r.release
	return errors.New("credential usage unavailable")
}
