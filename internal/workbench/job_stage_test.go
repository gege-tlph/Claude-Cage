package workbench

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Every progress message the workbench sets has a short stage for the status
// bar, so a new message cannot fall back to the job's name unnoticed.
func TestEveryProgressMessageHasAStage(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	literal := regexp.MustCompile(`setMessage\(("(?:[^"\\]|\\.)*")`)
	seen := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range literal.FindAllSubmatch(source, -1) {
			message, err := strconv.Unquote(string(match[1]))
			if err != nil {
				t.Fatalf("%s: %s: %v", file, match[1], err)
			}
			seen++
			if stage := jobStage("unknown-kind", message); stage == jobName("unknown-kind") {
				t.Errorf("%s: no stage for %q", file, message)
			}
		}
	}
	if seen < 40 {
		t.Fatalf("found only %d progress messages", seen)
	}
}

func TestStagesAreShortNames(t *testing.T) {
	for _, entry := range jobStages {
		if n := len([]rune(entry.stage)); n == 0 || n > 18 || strings.ContainsAny(entry.stage, "，。…；") {
			t.Errorf("stage %q for %q is not a short name", entry.stage, entry.prefix)
		}
	}
	if got := jobStage("settings", "设置已保存，正在启动浏览器、编辑器与共享文件…"); got != "启动浏览器" {
		t.Fatalf("saved-settings lead: %q", got)
	}
	if got := jobStage("start", "正在准备…"); got != "启动环境" {
		t.Fatalf("unknown message falls back to the job: %q", got)
	}
}

func TestFinishedStage(t *testing.T) {
	for _, tc := range []struct {
		job  Job
		want string
	}{
		{Job{Kind: "start", Message: "已完成"}, ""},
		{Job{Kind: "start", Canceled: true}, "已取消"},
		{Job{Kind: "start", Error: "x", Message: "启动环境失败"}, "启动环境失败"},
		{Job{Kind: "start", Error: "x", Message: "环境已启动，联网检查未通过"}, "联网检查未通过"},
	} {
		if got := finishedStage(tc.job); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.job, got, tc.want)
		}
	}
}
