package workbench

import (
	"net/http"
	"time"
)

// The operation log (操作日志) of this run: each job's progress messages with
// their time for the simple view, and each job's raw output for the advanced
// view. Only the last hour is kept, and nothing is written to disk; the
// diagnostics store keeps its own record.
const (
	jobLogAge     = time.Hour
	jobLogEntries = 1000
	jobLogOutputs = 20
)

type jobLogEntry struct {
	Time    time.Time `json:"time"`
	Job     string    `json:"job"`
	Kind    string    `json:"kind"`
	Stage   string    `json:"stage,omitempty"`
	Message string    `json:"message"`
	// State marks the entry that ends a job: done, failed or canceled.
	State string `json:"state,omitempty"`
}

type jobLogOutput struct {
	Job     string     `json:"job"`
	Kind    string     `json:"kind"`
	Started time.Time  `json:"started"`
	Ended   *time.Time `json:"ended,omitempty"`
	Log     string     `json:"log"`
}

type jobLogView struct {
	Entries []jobLogEntry  `json:"entries"`
	Outputs []jobLogOutput `json:"outputs"`
}

type jobLog struct {
	entries []jobLogEntry
	outputs []jobLogOutput
	now     func() time.Time
}

func (l *jobLog) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

func (l *jobLog) prune(now time.Time) {
	cut := now.Add(-jobLogAge)
	first := 0
	for first < len(l.entries) && l.entries[first].Time.Before(cut) {
		first++
	}
	if len(l.entries)-first > jobLogEntries {
		first = len(l.entries) - jobLogEntries
	}
	l.entries = append([]jobLogEntry(nil), l.entries[first:]...)
	kept := l.outputs[:0]
	for _, output := range l.outputs {
		if output.Ended == nil || !output.Ended.Before(cut) {
			kept = append(kept, output)
		}
	}
	if len(kept) > jobLogOutputs {
		kept = kept[len(kept)-jobLogOutputs:]
	}
	l.outputs = kept
}

func (l *jobLog) add(job Job, message, state string) {
	now := l.clock()
	if n := len(l.entries); n > 0 && state == "" && l.entries[n-1].Job == job.ID && l.entries[n-1].Message == message {
		return
	}
	l.entries = append(l.entries, jobLogEntry{Time: now, Job: job.ID, Kind: job.Kind, Stage: job.Stage, Message: message, State: state})
	l.prune(now)
}

// begin records a new job; a running job it replaced ends as canceled.
func (l *jobLog) begin(replaced, next Job) {
	if replaced.ID != "" && replaced.Canceled && replaced.SupersededBy == next.ID {
		l.end(replaced)
	}
	l.outputs = append(l.outputs, jobLogOutput{Job: next.ID, Kind: next.Kind, Started: l.clock()})
	l.add(next, jobName(next.Kind), "")
}

func (l *jobLog) progress(job Job) { l.add(job, job.Message, "") }

// end records how a job ended and keeps its output.
func (l *jobLog) end(job Job) {
	state, message := "done", job.Message
	switch {
	case job.Canceled:
		state = "canceled"
	case job.Error != "":
		state, message = "failed", job.Error
	}
	l.add(job, message, state)
	ended := l.clock()
	for i := range l.outputs {
		if l.outputs[i].Job == job.ID {
			l.outputs[i].Log, l.outputs[i].Ended = job.Log, &ended
		}
	}
}

// logView copies the log; the running job's output is read live.
func (m *jobManager) logView() jobLogView {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.log.prune(m.log.clock())
	view := jobLogView{Entries: append([]jobLogEntry{}, m.log.entries...), Outputs: append([]jobLogOutput{}, m.log.outputs...)}
	for i := range view.Outputs {
		if view.Outputs[i].Job == m.state.ID && m.state.Running {
			view.Outputs[i].Log = m.state.Log
		}
	}
	return view
}

func (s *Server) jobLog(w http.ResponseWriter, r *http.Request) { jsonReply(w, s.jobs.logView()) }
