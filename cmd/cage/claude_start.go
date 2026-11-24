package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/sylvinus/agent-vm/internal/workbench"
)

// environmentStarter is what `cage claude` uses to start a stopped
// environment; tests replace it.
type environmentStarter struct {
	running func() bool
	// service starts the controller when no window or service runs it.
	service func(context.Context) error
	start   func(context.Context) (workbench.Job, error)
	job     func(context.Context, string) (workbench.Job, error)
	poll    time.Duration
}

func controllerStarter(path string) environmentStarter {
	decode := func(data json.RawMessage, err error) (workbench.Job, error) {
		var job workbench.Job
		if err == nil {
			err = json.Unmarshal(data, &job)
		}
		return job, err
	}
	return environmentStarter{
		running: func() bool { return workbench.EnvironmentRunning(path) },
		service: func(ctx context.Context) error { return workbench.AgentStartService(ctx, path) },
		start: func(ctx context.Context) (workbench.Job, error) {
			return decode(workbench.AgentCall(ctx, path, "environment.start", nil))
		},
		job: func(ctx context.Context, id string) (workbench.Job, error) {
			return decode(workbench.AgentJob(ctx, path, id))
		},
		poll: time.Second,
	}
}

// ensureEnvironment starts the environment before `cage claude` when it is
// not running (#861). The start goes through the controller, as the window's
// start button does, so the proxy, guest services and network check run as
// usual. A running environment is left as it is.
func ensureEnvironment(ctx context.Context, s environmentStarter, stderr io.Writer) error {
	if s.running() {
		return nil
	}
	fmt.Fprintln(stderr, "cage: the environment is not running; starting it…")
	if err := s.service(ctx); err != nil {
		return err
	}
	job, err := s.start(ctx)
	if err != nil {
		return err
	}
	shown := ""
	for job.Running {
		if job.Message != "" && job.Message != shown {
			shown = job.Message
			fmt.Fprintln(stderr, "cage: "+shown)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the environment did not start in time; it may still be starting (cage agent job %s)", job.ID)
		case <-time.After(s.poll):
		}
		if job, err = s.job(ctx, job.ID); err != nil {
			return err
		}
	}
	if job.Canceled {
		return errors.New("starting the environment was canceled")
	}
	if job.Error != "" {
		return errors.New(job.Error)
	}
	return nil
}
