package workbench

import (
	"context"
	"errors"
	"testing"
)

// A Desktop-only account (Desktop discovery, CodeUnlinked) has no Code login:
// no check may probe it, and a status an older check saved recovers (#970).
func TestClaudeDesktopOnlyAccountDoesNotProbeCodeUsage(t *testing.T) {
	s, list := usageServer(t, 2)
	desktop, code := &list.Accounts[0], &list.Accounts[1]
	desktop.CodeUnlinked, desktop.Status, desktop.UsageStatus = true, "changed", "identity_changed"
	code.Status = "changed"
	if err := s.saveClaudeAccounts(list); err != nil {
		t.Fatal(err)
	}
	s.claudeAccounts.usageProbe = func(_ context.Context, id, _ string) (claudeGuestSnapshot, error) {
		if id == desktop.guestID() {
			t.Error("probed the Code usage of a Desktop-only account")
		}
		return claudeGuestSnapshot{}, errors.New("not reached")
	}
	loaded, err := s.loadClaudeAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if a := loaded.find(desktop.ID); a.Status != "signed_out" {
		t.Fatalf("Desktop-only account kept status %q", a.Status)
	}
	if a := loaded.find(code.ID); a.Status != "changed" {
		t.Fatalf("a real Code identity change was cleared: %q", a.Status)
	}
	if err := s.refreshClaudeUsage(context.Background(), claudeAccountInput{ID: desktop.ID}); err == nil {
		t.Fatal("a manual usage check accepted a Desktop-only account")
	}
	target := claudeUsageTarget{desktop.ID, desktop.guestID(), claudeIdentityHash(desktop.Email, desktop.Organization)}
	if s.usageOwner(loaded, target) != nil {
		t.Fatal("a usage result could still be recorded on a Desktop-only account")
	}
	// Once Code is authorized the account takes part again.
	loaded.find(desktop.ID).CodeUnlinked = false
	if s.usageOwner(loaded, target) == nil {
		t.Fatal("a linked account lost its usage owner")
	}
}
