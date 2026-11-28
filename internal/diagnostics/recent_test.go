package diagnostics

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRecentKeepsTheDefaultStoresEventsByLevel(t *testing.T) {
	previous := Default()
	t.Cleanup(func() {
		SetDefault(previous)
		recent = recentLog{}
	})
	recent = recentLog{}
	store := New(t.TempDir())
	SetDefault(store)
	store.Record("debug", "vm-hostagent", "clock in sync")
	store.Record("info", "task/start", "token=secret-value started")
	store.Record("warn", "proxy", "connection refused")
	store.Record("error", "api", "failed")
	// Another store is not this run's log.
	New(t.TempDir()).Record("error", "other", "not shown")

	levels := func(events []Event) string {
		var out []string
		for _, event := range events {
			out = append(out, event.Level+":"+event.Source)
		}
		return strings.Join(out, ",")
	}
	if got := levels(Recent("debug", 10)); got != "debug:vm-hostagent,info:task/start,warn:proxy,error:api" {
		t.Fatalf("debug: %s", got)
	}
	if got := levels(Recent("warn", 10)); got != "warn:proxy,error:api" {
		t.Fatalf("warn: %s", got)
	}
	// An unknown level reads as info; max keeps the newest.
	if got := levels(Recent("loud", 2)); got != "warn:proxy,error:api" {
		t.Fatalf("max: %s", got)
	}
	if events := Recent("info", 10); strings.Contains(events[0].Message, "secret-value") {
		t.Fatalf("not redacted: %q", events[0].Message)
	}
}

func TestRecentDropsOldAndExcessEvents(t *testing.T) {
	t.Cleanup(func() { recent = recentLog{} })
	recent = recentLog{}
	old := time.Now().Add(-recentAge - time.Minute).UTC().Format(time.RFC3339Nano)
	recent.add([]Event{{Time: old, Level: "error", Source: "old", Message: "gone"}})
	now := time.Now().UTC().Format(time.RFC3339Nano)
	batch := make([]Event, recentCount+5)
	for i := range batch {
		batch[i] = Event{Time: now, Level: "info", Source: "task", Message: fmt.Sprint(i)}
	}
	recent.add(batch)
	events := Recent("debug", recentCount*2)
	if len(events) != recentCount || events[0].Message != "5" {
		t.Fatalf("kept %d, first %q", len(events), events[0].Message)
	}
	recent.add([]Event{{Time: now, Level: "info", Source: "big", Message: strings.Repeat("x", recentBytes)}})
	if recent.bytes > recentBytes {
		t.Fatalf("bytes %d over the bound", recent.bytes)
	}
}
