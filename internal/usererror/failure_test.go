package usererror

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestFailureReadsAsThreeLines(t *testing.T) {
	cause := errors.New("exit status 1")
	f := Fail("启动环境", "原因是虚拟机启动后，SSH 连接超时（10 分钟）", "重试启动", cause, ToolStart, ToolDiagnostics)
	if got := f.Error(); got != "启动环境失败，原因是虚拟机启动后，SSH 连接超时（10 分钟）。解决建议：重试启动。" {
		t.Fatalf("joined: %q", got)
	}
	if !errors.Is(f, cause) || f.Level != LevelError {
		t.Fatalf("cause or level lost: %+v", f)
	}
	notice := (&Failure{Lead: "时区已保存", Reason: "重启环境后生效（Asia/Tokyo）", Next: "重启环境"}).WithLevel(LevelInfo)
	if got := notice.Error(); got != "时区已保存，重启环境后生效（Asia/Tokyo）。下一步：重启环境。" {
		t.Fatalf("notice: %q", got)
	}
	wrapped := errors.Join(errors.New("context"), f)
	if found, ok := AsFailure(wrapped); !ok || found != f {
		t.Fatal("AsFailure missed a joined failure")
	}
}

func TestUnrecognizedQuotesTheLastErrorAndTheStage(t *testing.T) {
	f := Unrecognized("启动环境", "browser-display.service: Main process exited, code=exited, status=1/FAILURE", "重试启动", nil, ToolStart)
	f.StuckAt("启动浏览器")
	f.StuckAt("启动浏览器")
	want := "启动环境失败，卡在启动浏览器，原因未识别（最后一条错误：browser-display.service: Main process exited, code=exited, status=1/FAILURE）。解决建议：重试启动。"
	if got := f.Error(); got != want {
		t.Fatalf("got %q", got)
	}
	known := Fail("启动环境", "原因是 KVM 不可用", "重试启动", nil)
	known.StuckAt("启动浏览器")
	if strings.Contains(known.Reason, "卡在") {
		t.Fatal("a known cause got a stage")
	}
	long := Unrecognized("启动环境", strings.Repeat("x", 300), "", nil)
	if n := len([]rune(long.Raw)); n != rawLimit+1 || !strings.HasSuffix(long.Raw, "…") {
		t.Fatalf("raw not shortened: %d", n)
	}
}

func TestDurationReadsLikeTheInterface(t *testing.T) {
	for d, want := range map[time.Duration]string{
		30 * time.Second:               "30 秒",
		10 * time.Minute:               "10 分钟",
		4*time.Minute + 30*time.Second: "4 分 30 秒",
		2 * time.Hour:                  "2 小时",
		time.Hour + 5*time.Minute:      "1 小时 5 分钟",
		0:                              "",
	} {
		if got := Duration(d); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}
