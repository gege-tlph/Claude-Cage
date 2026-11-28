package workbench

import (
	"strconv"
	"strings"

	"github.com/sylvinus/agent-vm/internal/usererror"
)

// The full package's installation data (internal/cli setup_offline.go): the
// setup checks it, puts the Debian image in Lima's cache and copies the rest
// into the guest. Nothing downloads, so no 「正在下载」 step appears.

// offlineSetupProgress names those steps from the setup's output.
func offlineSetupProgress(line string) *SetupProgress {
	switch line {
	case "Checking the installation files...":
		return &SetupProgress{Phase: "image", Label: "正在校验安装文件"}
	case "Checking the Debian image...":
		return &SetupProgress{Phase: "image", Label: "正在校验 Debian 镜像"}
	case "Copying installation files...":
		return &SetupProgress{Phase: "packages", Label: "正在复制安装文件"}
	}
	fields := strings.Fields(line)
	if len(fields) != 4 || fields[0] != "CAGE_OFFLINE_BYTES" {
		return nil
	}
	received, err := strconv.ParseInt(fields[2], 10, 64)
	total, te := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || te != nil || received < 0 || total <= 0 || received > total {
		return nil
	}
	p := &SetupProgress{Received: &received, Total: total}
	switch fields[1] {
	case "verify":
		p.Phase, p.Label, p.Operation = "image", "正在校验安装文件", "verify"
	case "image":
		p.Phase, p.Label, p.Operation = "image", "正在校验 Debian 镜像", "verify"
	case "copy":
		p.Phase, p.Label = "packages", "正在复制安装文件"
	default:
		return nil
	}
	return p
}

// offlineDataFailure: the installation data failed its check. Continuing
// would fail the same way until the full package's installer has put the
// files back.
func offlineDataFailure(stage string, cause error, output string) *usererror.Failure {
	var code, kind string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for i, f := range fields {
			if f == "CAGE_OFFLINE_FAILED" && i+1 < len(fields) {
				code, kind = fields[i+1], ""
				if i+2 < len(fields) {
					kind = fields[i+2]
				}
			}
		}
	}
	if code == "" {
		return nil
	}
	reason := "原因是" + stage + "时完整包里的安装文件与清单不符"
	switch {
	case code == "manifest":
		reason = "原因是" + stage + "时读不出完整包的安装文件清单"
	case code == "image":
		reason = "原因是" + stage + "时完整包里的 Debian 镜像不是这个版本使用的镜像"
	case code == "corrupt" && kind == "apt":
		reason = "原因是" + stage + "时完整包里的软件包与清单不符"
	case code == "corrupt" && kind == "tools":
		reason = "原因是" + stage + "时完整包里的工具安装包与清单不符"
	case code == "corrupt" && kind == "image":
		reason = "原因是" + stage + "时完整包里的 Debian 镜像校验不符"
	}
	return usererror.Because(reason, "重新运行完整包的安装程序后继续安装", cause, usererror.ToolStart, usererror.ToolLog)
}
