package workbench

import (
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/sylvinus/agent-vm/internal/vm"
)

// An install failure keeps the operation for the caller and puts the install
// step into the reason (failure-text-audit group 3, I1–I7).
func TestInstallFailureNamesTheStep(t *testing.T) {
	healthyHost(t)
	cause := errors.New("exit status 1")
	const apt = "APT_FAILED exit=100 command=apt-get install -y git curl\n"
	for _, tc := range []struct {
		name, stage, output, reason string
		tools                       []string
	}{
		{"mirrors one way", "下载 Debian 13 镜像", vm.ImageDownloadMarker + " official:timeout:kept nju:timeout:none\n",
			"原因是下载 Debian 13 镜像时 2 个下载地址都失败（连接超时）", []string{"start", "log"}},
		{"mirrors each their way", "下载 Debian 13 镜像", vm.ImageDownloadMarker + " official:timeout:none nju:http-404:none\n",
			"原因是下载 Debian 13 镜像时 2 个下载地址都失败（Debian 官方源：连接超时；南京大学镜像：HTTP 404）", []string{"start", "log"}},
		{"system proxy", "下载 Debian 13 镜像", vm.ImageDownloadMarker + " official:proxy:none nju:proxy:none\n",
			"原因是下载 Debian 13 镜像时连不上电脑的系统代理（2 个下载地址都失败）", []string{"start", "log"}},
		{"checksum", "下载 Debian 13 镜像", vm.ImageDownloadMarker + " official:checksum:none\n",
			"原因是下载 Debian 13 镜像时下载的镜像校验不符（已丢弃）", []string{"start", "settings:network"}},
		{"apt dns", "安装 Linux 基础软件包", "  Temporary failure resolving 'deb.debian.org'\n" + apt,
			"原因是安装 Linux 基础软件包时解析不了软件源域名", []string{"settings:network", "start"}},
		{"apt eof", "安装 Linux 基础软件包", "OpenSSL error: unexpected eof while reading\nAPT_PARTIAL bytes=5242880\n" + apt,
			"原因是安装 Linux 基础软件包时下载连接中断", []string{"start", "log"}},
		{"apt dpkg", "安装 Linux 基础软件包", "E: dpkg was interrupted, you must manually run 'sudo dpkg --configure -a'\n" + apt,
			"原因是安装 Linux 基础软件包时，上次中断的安装没能补完（dpkg）", []string{"start", "log"}},
		{"apt unknown", "安装 Python", "something odd happened\n" + apt,
			"卡在安装 Python，原因未识别", []string{"start", "log", "diagnostics"}},
		{"curl tls", "下载 Chromium 浏览器", "curl: (35) OpenSSL SSL_connect: SSL_ERROR_SYSCALL\n",
			"原因是下载 Chromium 浏览器时 TLS 握手中断", []string{"tls12-retry", "settings:network"}},
		{"start rules apply", "启动 Linux 基础环境", "FATA[0605] failed to satisfy the essential requirement 1 of 2 `ssh`: hint\n",
			"原因是虚拟机启动后，SSH 连接超时", []string{"start", "diagnostics"}},
		{"unknown", "配置桌面服务", "custom setup ended\nexit status 3\n",
			"卡在配置桌面服务，原因未识别", []string{"start", "log", "diagnostics"}},
	} {
		f := installFailure(tc.stage, cause, tc.output, failureFacts{})
		if f.Lead != "" || f.Reason != tc.reason || !reflect.DeepEqual(f.Tools, tc.tools) {
			t.Errorf("%s: %+v, want %q %v", tc.name, f, tc.reason, tc.tools)
		}
	}
	if f := installFailure("安装 Linux 基础软件包", cause, "OpenSSL error: unexpected eof while reading\nAPT_PARTIAL bytes=5242880\n"+apt, failureFacts{}); f.Next != "继续安装，APT 缓存里 5.0 MiB 未完成的软件包会复用" {
		t.Fatalf("kept download: %q", f.Next)
	}
	if installStage("正在下载 Debian 13 镜像…") != "下载 Debian 13 镜像" {
		t.Fatal("stage")
	}
}

// First run calls the start 「安装」 and its retry 「继续安装」; the workbench
// keeps 「启动环境」.
func TestFirstRunCallsItInstall(t *testing.T) {
	s := testServer(t)
	for initiator, want := range map[string][2]string{jobInitiatorSetup: {"安装失败", "continue-install"}, jobInitiatorDesktop: {"启动环境失败", "start"}} {
		var m jobManager
		var operation sync.Mutex
		_, err := m.submitWithInitiator(&operation, "start", initiator, 0, nil, nil, func() error {
			return s.describeFailure("start", initiator, installFailure("下载 Debian 13 镜像", errors.New("exit status 1"), vm.ImageDownloadMarker+" official:timeout:none\n", failureFacts{}))
		})
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, func() bool { return !m.snapshot().Running })
		f := m.snapshot().Failure
		if f == nil || f.Lead != want[0] || f.Tools[0] != want[1] {
			t.Errorf("%s: %+v", initiator, f)
		}
	}
}
