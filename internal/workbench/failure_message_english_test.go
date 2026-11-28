package workbench

import (
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/sylvinus/agent-vm/internal/usererror"
)

// The English interface splits "<action>失败：<reason>。<next>" and translates
// each part (ui/src/backend-message.mjs). Every message these failures build
// must split that way, and each part needs an English entry.
func TestFailureMessagesHaveEnglish(t *testing.T) {
	data, err := os.ReadFile("../../ui/src/locales/en.json")
	if err != nil {
		t.Fatal(err)
	}
	var english map[string]string
	if err := json.Unmarshal(data, &english); err != nil {
		t.Fatal(err)
	}
	split := regexp.MustCompile(`^([^：。]+)失败：([^。]+)。([\s\S]*)$`)
	value := regexp.MustCompile(`HTTP \d+`)
	check := func(message string) {
		t.Helper()
		m := split.FindStringSubmatch(message)
		if m == nil {
			t.Errorf("message does not split into action, reason and next step: %q", message)
			return
		}
		reason := value.ReplaceAllString(m[2], "HTTP {0}")
		for _, part := range []string{m[1], reason, m[3]} {
			if part != "" && english[part] == "" {
				t.Errorf("no English for %q (in %q)", part, message)
			}
		}
	}
	for _, rule := range failureRules {
		output := rule.patterns[0]
		message := executionFailure("启动环境", errors.New("exit status 1"), output).Error()
		if !strings.Contains(message, strings.TrimRight(rule.reason, "。；;")) {
			t.Errorf("pattern %q did not select its own rule: %q", output, message)
		}
		check(message)
	}
	check(executionFailure("启动环境", errors.New("exit status 1"), "unknown").Error())
	// Failures in three parts (desktop_failure.go): each part needs English.
	checkFailure := func(err error) {
		t.Helper()
		f, ok := usererror.AsFailure(err)
		if !ok {
			t.Errorf("not a failure: %v", err)
			return
		}
		action := strings.TrimSuffix(strings.TrimSuffix(f.Lead, "失败"), " ")
		for _, part := range []string{action, value.ReplaceAllString(f.Reason, "HTTP {0}"), f.Next} {
			if part != "" && english[part] == "" && english[value.ReplaceAllString(part, "HTTP {0}")] == "" {
				t.Errorf("no English for %q (in %q)", part, err.Error())
			}
		}
	}
	for _, status := range []int{401, 404, 409, 413, 429, 502, 504} {
		checkFailure(serviceHTTPFailure("传递文件", status))
	}
	for _, sent := range []string{"上传窗口已关闭，请重新选择文件", "原文件窗口已关闭", "请先完成已打开的文件窗口", "每个文件不能超过 2 GB", "项目文件夹尚未连接，请重新打开项目", "目标没有接收文件，请点击支持附件的输入框后重试", "上传已取消", "当前窗口没有请求文件夹", "文件夹正在交给应用", "文件位置无法读取", "下载服务不可用", "文件请求过大", "请选择项目文件夹"} {
		err := fileServiceFailure(400, []byte(sent))
		if !strings.HasPrefix(err.Error(), "传递文件失败，原因是") || strings.Contains(err.Error(), "HTTP") {
			t.Errorf("%q was not recognized: %q", sent, err.Error())
		}
		checkFailure(err)
	}
}
