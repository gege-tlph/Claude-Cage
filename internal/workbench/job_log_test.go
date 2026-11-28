package workbench

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestJobLogRecordsProgressAndOutcome(t *testing.T) {
	var m jobManager
	var operation sync.Mutex
	release := make(chan struct{})
	job, err := m.submit(&operation, "start", 0, nil, nil, func() error {
		m.execution().message("正在验证代理出口…")
		m.execution().message("正在验证代理出口…")
		_, _ = m.execution().Write([]byte("lima: starting\n"))
		m.execution().message("正在启动浏览器、编辑器与共享文件…")
		<-release
		return errors.New("browser did not start")
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return m.snapshot().Stage == "启动浏览器" })
	running := m.logView()
	if len(running.Outputs) != 1 || !strings.Contains(running.Outputs[0].Log, "lima: starting") {
		t.Fatalf("running output %+v", running.Outputs)
	}
	close(release)
	waitFor(t, func() bool { return !m.snapshot().Running })
	view := m.logView()
	var lines []string
	for _, e := range view.Entries {
		lines = append(lines, e.Stage+"|"+e.State)
		if e.Job != job.ID {
			t.Fatalf("entry of another job: %+v", e)
		}
	}
	want := "启动环境||验证出口||启动浏览器||启动环境失败|failed"
	if got := strings.Join(lines, "|"); got != want {
		t.Fatalf("entries %s, want %s", got, want)
	}
	if last := view.Entries[len(view.Entries)-1]; !strings.HasPrefix(last.Message, "启动环境失败") {
		t.Fatalf("failure entry %q", last.Message)
	}
	if m.snapshot().Stage != "启动环境失败" || view.Outputs[0].Ended == nil || !strings.Contains(view.Outputs[0].Log, "lima: starting") {
		t.Fatalf("finished job %+v output %+v", m.snapshot(), view.Outputs[0])
	}
}

func TestJobLogEndsReplacedJobAndKeepsAnHour(t *testing.T) {
	now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	l := jobLog{now: func() time.Time { return now }}
	first := Job{ID: "a", Kind: "start", Stage: "启动环境"}
	l.begin(Job{}, first)
	replaced := first
	replaced.Canceled, replaced.SupersededBy = true, "b"
	now = now.Add(30 * time.Minute)
	l.begin(replaced, Job{ID: "b", Kind: "stop"})
	if n := len(l.entries); n != 3 || l.entries[1].State != "canceled" || l.entries[1].Job != "a" {
		t.Fatalf("entries %+v", l.entries)
	}
	if l.outputs[0].Ended == nil {
		t.Fatal("replaced job's output not ended")
	}
	now = now.Add(61 * time.Minute)
	l.end(Job{ID: "b", Kind: "stop", Message: "已完成"})
	if len(l.entries) != 1 || l.entries[0].State != "done" {
		t.Fatalf("an hour later %+v", l.entries)
	}
	if len(l.outputs) != 1 || l.outputs[0].Job != "b" {
		t.Fatalf("outputs %+v", l.outputs)
	}
}

// A job that ended canceled and is then replaced shows one 「已取消」.
func TestJobLogDoesNotEndAnEndedJobAgain(t *testing.T) {
	now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	l := jobLog{now: func() time.Time { return now }}
	first := Job{ID: "a", Kind: "start", Stage: "启动环境"}
	l.begin(Job{}, first)
	canceled := first
	canceled.Canceled, canceled.Message = true, "已取消"
	l.end(canceled)
	ended := *l.outputs[0].Ended
	now = now.Add(time.Minute)
	canceled.SupersededBy = "b"
	l.begin(canceled, Job{ID: "b", Kind: "start"})
	states := 0
	for _, entry := range l.entries {
		if entry.Job == "a" && entry.State == "canceled" {
			states++
		}
	}
	if states != 1 || !l.outputs[0].Ended.Equal(ended) {
		t.Fatalf("entries %+v, ended %v", l.entries, l.outputs[0].Ended)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
