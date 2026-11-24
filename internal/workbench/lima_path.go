package workbench

import (
	"path/filepath"

	"github.com/sylvinus/agent-vm/internal/cage"
	"github.com/sylvinus/agent-vm/internal/paths"
	"github.com/sylvinus/agent-vm/internal/usererror"
	"github.com/sylvinus/agent-vm/internal/vmname"
)

const (
	limaPathTooLongReason = "环境目录路径太长，虚拟机的本机套接字超出系统长度限制"
	limaPathTooLongNext   = "请把配置目录或环境存储改到更短的路径（例如 D:\\Cage）后重试。"
	// The longest VM name setup creates under the Lima home.
	limaBuildName = "base-building"
)

// requireLimaPathFits refuses to start when the Lima home is so deep that the
// base build's sockets would exceed UNIX_PATH_MAX. Lima would only refuse it
// later, after the image download, with a summary that hid the reason (#913).
func (s *Server) requireLimaPathFits() error {
	home, logical := s.commandLimaHome()
	if int(vmname.HomeLimit(home, logical)) >= len(limaBuildName) {
		return nil
	}
	return usererror.New("启动环境", limaPathTooLongReason, limaPathTooLongNext, nil)
}

// commandLimaHome is the Lima home cmd/cage gives Lima for this profile: with
// custom storage the profile is resolved once and the rest stays logical
// (CAGE_LOGICAL_LIMA_HOME). Inspecting VMs in this process must use the same
// home, or a moved storage's VMs look missing.
func (s *Server) commandLimaHome() (home string, logical bool) {
	home = filepath.Join(s.profile, "state", "lima")
	if location, err := cage.GetStorageLocation(s.profile); err == nil && location.Custom {
		if real, err := paths.Resolve(s.profile); err == nil {
			return filepath.Join(real, "state", "lima"), true
		}
	}
	return home, false
}
