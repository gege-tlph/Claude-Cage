package workbench

import (
	"errors"
	"strings"
	"testing"

	"github.com/sylvinus/agent-vm/internal/usererror"
)

func TestOfflineSetupProgressNamesItsSteps(t *testing.T) {
	for line, want := range map[string][2]string{
		"Checking the installation files...": {"image", "正在校验安装文件"},
		"CAGE_OFFLINE_BYTES verify 10 100":   {"image", "正在校验安装文件"},
		"CAGE_OFFLINE_BYTES image 10 100":    {"image", "正在校验 Debian 镜像"},
		"Copying installation files...":      {"packages", "正在复制安装文件"},
		"  CAGE_OFFLINE_BYTES copy 100 100":  {"packages", "正在复制安装文件"},
	} {
		p := parseSetupProgress(line)
		if p == nil || p.Phase != want[0] || p.Label != want[1] {
			t.Errorf("%q = %+v", line, p)
		}
	}
	if p := parseSetupProgress("CAGE_OFFLINE_BYTES copy 101 100"); p != nil {
		t.Errorf("more than the total: %+v", p)
	}
	if p := parseSetupProgress("CAGE_OFFLINE_BYTES verify 5 10"); p == nil || p.Operation != "verify" || *p.Received != 5 || p.Total != 10 {
		t.Errorf("verify bytes: %+v", p)
	}
}

func TestOfflineDataFailureAsksForTheInstaller(t *testing.T) {
	output := "Checking the installation files...\nError: CAGE_OFFLINE_FAILED corrupt apt\noffline data file does not match its manifest: [path]\n"
	f := installFailure(installStage("正在校验安装文件"), errors.New("exit status 1"), output, failureFacts{})
	if f.Reason != "原因是校验安装文件时完整包里的软件包与清单不符" || f.Next != "重新运行完整包的安装程序后继续安装" {
		t.Fatalf("failure: %+v", f)
	}
	if strings.Join(f.Tools, ",") != usererror.ToolStart+","+usererror.ToolLog {
		t.Fatalf("tools: %v", f.Tools)
	}
	if f := offlineDataFailure("安装", nil, "Error: CAGE_OFFLINE_FAILED image\n"); f == nil || !strings.Contains(f.Reason, "不是这个版本使用的镜像") {
		t.Fatalf("image: %+v", f)
	}
	if f := offlineDataFailure("安装", nil, "Installing base packages...\n"); f != nil {
		t.Fatalf("no marker: %+v", f)
	}
}
