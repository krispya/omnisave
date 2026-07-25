package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func newTestConnectionModel(failure *ConnectionFailure) connectionModel {
	model := newConnectionModel(context.Background(), "http://localhost:8080", func(context.Context) *ConnectionFailure {
		return failure
	})
	model.spinner.Style = accentStyle.UnsetForeground()
	return model
}

func checked(t *testing.T, model connectionModel, failure *ConnectionFailure) connectionModel {
	t.Helper()
	updated, _ := model.Update(connectionCheckedMsg{failure: failure})
	return updated.(connectionModel)
}

func TestAServerThatAnswersLeavesNothingOnScreen(t *testing.T) {
	model := checked(t, newTestConnectionModel(nil), nil)

	if !model.connected {
		t.Fatal("expected a check that answered to report the connection")
	}
	if view := model.View(); view != "" {
		t.Fatalf("expected the connection check to leave no trace, got %q", view)
	}
}

func TestAPendingCheckHoldsTheActivityLineRatherThanThePanel(t *testing.T) {
	view := ansi.Strip(newTestConnectionModel(nil).View())

	if strings.Contains(view, "▲") || !strings.Contains(view, "Contacting the Omnisave server") {
		t.Fatalf("expected a compact activity line while the check runs, got %q", view)
	}
}

func TestTheDisconnectedPanelShowsTheBrandTheCauseAndTheRetryKey(t *testing.T) {
	failure := &ConnectionFailure{Cause: "connection refused", Retry: true}
	model := checked(t, newTestConnectionModel(failure), failure)

	view := ansi.Strip(model.View())

	expected := []string{
		"▲ Omnisave",
		"- Server:  http://localhost:8080",
		"✗ Disconnected  connection refused",
		"r retry · q quit",
	}
	for _, text := range expected {
		if !strings.Contains(view, text) {
			t.Fatalf("expected the disconnected panel to contain %q, got:\n%s", text, view)
		}
	}
}

func TestRetryingContactsTheServerAgainAndConnects(t *testing.T) {
	failure := &ConnectionFailure{Cause: "connection refused", Retry: true}
	answering := false
	model := newConnectionModel(context.Background(), "http://localhost:8080", func(context.Context) *ConnectionFailure {
		if answering {
			return nil
		}
		return failure
	})
	model = checked(t, model, failure)

	retried, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if command == nil {
		t.Fatal("expected the retry key to contact the server again")
	}
	retrying := retried.(connectionModel)
	if !retrying.checking {
		t.Fatal("expected the retry to show the panel as reconnecting")
	}
	if view := ansi.Strip(retrying.View()); !strings.Contains(view, "Reconnecting") || !strings.Contains(view, "q cancel") {
		t.Fatalf("expected a reconnecting panel while the retry runs, got:\n%s", view)
	}

	answering = true
	settled := checked(t, retrying, retrying.check(context.Background()))
	if !settled.connected {
		t.Fatal("expected the retry to connect once the server answered")
	}
}

func TestAFailureRetryingCannotClearOffersTheFixInsteadOfTheKey(t *testing.T) {
	failure := &ConnectionFailure{
		Cause: "the server rejected this token",
		Fix:   "Run omnisave-client connect to reconnect this device",
	}
	model := checked(t, newTestConnectionModel(failure), failure)

	view := ansi.Strip(model.View())

	if !strings.Contains(view, "Run omnisave-client connect to reconnect this device") {
		t.Fatalf("expected the panel to carry the fix, got:\n%s", view)
	}
	if strings.Contains(view, "r retry") {
		t.Fatalf("expected no retry key for a failure retrying cannot clear, got:\n%s", view)
	}
	retried, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if command != nil || retried.(connectionModel).checking {
		t.Fatal("expected the retry key to do nothing when retrying cannot help")
	}
}

func TestQuittingKeepsThePanelAndDropsTheKeys(t *testing.T) {
	failure := &ConnectionFailure{Cause: "connection refused", Retry: true}
	model := checked(t, newTestConnectionModel(failure), failure)

	quit, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if command == nil {
		t.Fatal("expected q to end the view")
	}
	view := ansi.Strip(quit.(connectionModel).View())
	if !strings.Contains(view, "✗ Disconnected  connection refused") {
		t.Fatalf("expected the panel to stay as the record of the failure, got:\n%s", view)
	}
	if strings.Contains(view, "retry") || strings.Contains(view, "quit") {
		t.Fatalf("expected the keys to leave with the view, got:\n%s", view)
	}
}

func TestQuittingBeforeTheFirstAnswerLeavesNothingBehind(t *testing.T) {
	model := newTestConnectionModel(nil)

	quit, command := model.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if command == nil {
		t.Fatal("expected esc to end the view")
	}
	if view := quit.(connectionModel).View(); view != "" {
		t.Fatalf("expected a call to be called off to leave no panel, got %q", view)
	}
}
