package managedproxy

import "errors"

// Errors the workbench turns into three-line failures (failure-text-audit
// group 2): matched with errors.Is, never by their text.
var (
	// ErrCoreNotReady: the core did not answer within coreReadyTimeout.
	ErrCoreNotReady = errors.New("代理核心 15 秒内没有就绪")
	// ErrCoreExited: the core quit while starting.
	ErrCoreExited = errors.New("代理核心启动后立即退出")
	// ErrNoNodeSelected: no node is chosen at all.
	ErrNoNodeSelected = errors.New("尚未选择代理节点，请在网络与代理中添加来源并选择节点")
	// ErrNodeNotApplied: the chosen node differs from what the core runs.
	ErrNodeNotApplied = errors.New("代理节点尚未应用或配置不一致，请重新应用节点")
)
