package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sylvinus/agent-vm/internal/workbench"
)

func TestEnsureEnvironmentLeavesRunningEnvironment(t *testing.T) {
	called := false
	s := environmentStarter{
		running: func() bool { return true },
		service: func(context.Context) error { called = true; return nil },
		start:   func(context.Context) (workbench.Job, error) { called = true; return workbench.Job{}, nil },
	}
	var out bytes.Buffer
	if err := ensureEnvironment(context.Background(), s, &out); err != nil || called || out.Len() != 0 {
		t.Fatalf("running environment touched: %v %v %q", err, called, out.String())
	}
}

func TestEnsureEnvironmentStartsThroughController(t *testing.T) {
	var steps []string
	polls := []workbench.Job{
		{ID: "abc", Running: true, Message: "正在启动环境"},
		{ID: "abc", Running: true, Message: "正在启动环境"},
		{ID: "abc", Running: false, Message: "环境已就绪"},
	}
	s := environmentStarter{
		running: func() bool { return false },
		service: func(context.Context) error { steps = append(steps, "service"); return nil },
		start: func(context.Context) (workbench.Job, error) {
			steps = append(steps, "start")
			return workbench.Job{ID: "abc", Running: true, Message: "正在准备环境"}, nil
		},
		job: func(_ context.Context, id string) (workbench.Job, error) {
			steps = append(steps, "job "+id)
			job := polls[0]
			polls = polls[1:]
			return job, nil
		},
	}
	var out bytes.Buffer
	if err := ensureEnvironment(context.Background(), s, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Join(steps, ",") != "service,start,job abc,job abc,job abc" {
		t.Fatalf("steps: %v", steps)
	}
	// Each progress message is shown once.
	if got := out.String(); strings.Count(got, "正在启动环境") != 1 || !strings.Contains(got, "正在准备环境") {
		t.Fatalf("progress: %q", got)
	}
}

func TestEnsureEnvironmentReportsFailures(t *testing.T) {
	base := environmentStarter{
		running: func() bool { return false },
		service: func(context.Context) error { return nil },
		start:   func(context.Context) (workbench.Job, error) { return workbench.Job{ID: "abc"}, nil },
	}
	cases := map[string]func(*environmentStarter){
		"service": func(s *environmentStarter) {
			s.service = func(context.Context) error { return errors.New("controller unavailable") }
		},
		"start": func(s *environmentStarter) {
			s.start = func(context.Context) (workbench.Job, error) { return workbench.Job{}, errors.New("state_conflict") }
		},
		"job error": func(s *environmentStarter) {
			s.start = func(context.Context) (workbench.Job, error) {
				return workbench.Job{ID: "abc", Error: "代理未应用"}, nil
			}
		},
		"canceled": func(s *environmentStarter) {
			s.start = func(context.Context) (workbench.Job, error) { return workbench.Job{ID: "abc", Canceled: true}, nil }
		},
	}
	for name, change := range cases {
		s := base
		change(&s)
		if err := ensureEnvironment(context.Background(), s, &bytes.Buffer{}); err == nil {
			t.Errorf("%s: claude would run without an environment", name)
		}
	}
	// A start that never finishes ends with the context.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := base
	s.start = func(context.Context) (workbench.Job, error) { return workbench.Job{ID: "abc", Running: true}, nil }
	s.job = func(context.Context, string) (workbench.Job, error) {
		return workbench.Job{ID: "abc", Running: true}, nil
	}
	s.poll = 1 << 30
	if err := ensureEnvironment(ctx, s, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "abc") {
		t.Fatalf("timeout: %v", err)
	}
}
