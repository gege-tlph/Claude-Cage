package workbench

import "strings"

// jobStages names the stage a progress message belongs to, for the status
// bar: a short name, while the message itself goes to the operation log.
// Matched by prefix, in order, against the message without the 设置已保存
// lead. A message without an entry shows the job's name.
var jobStages = []struct{ prefix, stage string }{
	// Starting and stopping the environment.
	{messagePrefix("正在验证代理出口"), "验证出口"},
	{messagePrefix("正在应用出口所在地的时区与语言建议"), "应用时区"},
	{messagePrefix("代理出口无法通过检查，正在启动离线环境"), "启动环境"},
	{messagePrefix("首次准备 Linux 环境与开发工具"), "准备环境"},
	{messagePrefix("正在启动浏览器、编辑器与共享文件"), "启动浏览器"},
	{messagePrefix("正在重启环境以加载 VM 外网络门槛"), "重启环境"},
	{messagePrefix("正在停止环境"), "停止环境"},
	{messagePrefix("正在停止当前环境"), "停止环境"},
	{messagePrefix("正在连接项目文件夹"), "连接项目"},
	{messagePrefix("正在重新连接客机"), "连接环境"},
	// Proxy and exit.
	{messagePrefix("正在验证新出口"), "验证出口"},
	{messagePrefix("正在下载节点订阅"), "下载订阅"},
	{messagePrefix("正在验证节点配置"), "验证节点"},
	{messagePrefix("正在应用内置代理节点"), "应用节点"},
	{messagePrefix("节点已应用，正在重新检查环境出口"), "检查出口"},
	{messagePrefix("节点已应用；启动环境后完成联网检查"), "应用节点"},
	{messagePrefix("正在识别物理网卡并重新绑定代理"), "绑定网卡"},
	// Network checks.
	{messagePrefix("浏览器出口检测暂时未成功"), "检查出口"},
	{messagePrefix("正在首次绑定此出口节点"), "绑定出口"},
	{messagePrefix("正在核对静态 IP 或动态地区范围"), "检查出口"},
	{messagePrefix("正在验证离线约束"), "检查网络"},
	{messagePrefix("正在通过固定检测通道检查代理与 DNS"), "检查网络"},
	{messagePrefix("正在读取隔离浏览器的实际配置"), "检查网络"},
	{messagePrefix("正在演练代理失败"), "检查网络"},
	{messagePrefix("正在重新检查环境与出口"), "检查网络"},
	// Storage.
	{messagePrefix("正在恢复环境存储"), "恢复存储"},
	{messagePrefix("环境存储已恢复"), "恢复存储"},
	{messagePrefix("环境存储已重建"), "重建存储"},
	{messagePrefix("正在重建环境存储"), "重建存储"},
	{messagePrefix("正在检查环境存储"), "检查存储"},
	// Device reset.
	{messagePrefix("正在读取当前环境身份"), "生成身份"},
	{messagePrefix("正在客机内停止客户端"), "清理数据"},
	{messagePrefix("正在停止环境以应用新硬件身份"), "停止环境"},
	{messagePrefix("正在以全新身份重新启动环境"), "启动环境"},
	{messagePrefix("正在准备新机配置"), "准备新机"},
	{messagePrefix("新机构建未完成"), "回滚环境"},
	{messagePrefix("新容器重建未完成"), "回滚环境"},
	{messagePrefix("正在从本地缓存镜像重建全新客机"), "重建环境"},
	{messagePrefix("正在基于固定基础镜像创建全新容器环境"), "重建环境"},
	{messagePrefix("新机已验证就绪"), "清理归档"},
	{messagePrefix("正在停止环境以回滚旧环境归档"), "停止环境"},
	{messagePrefix("正在启动恢复后的"), "启动环境"},
	// Environment components and apps.
	{messagePrefix("正在重启浏览器与编辑器组件"), "重启组件"},
	{messagePrefix("正在恢复环境组件"), "恢复组件"},
	{messagePrefix("正在更新 Chromium、编辑器和开发工具"), "更新组件"},
	{messagePrefix("正在下载并安装 Claude Desktop"), "安装 Claude Desktop"},
	{messagePrefix("正在关闭 Claude Desktop"), "关闭 Claude Desktop"},
	{messagePrefix("正在打开 Claude Desktop"), "打开 Claude Desktop"},
	{messagePrefix("正在安装 Tailscale"), "安装 Tailscale"},
	{messagePrefix("正在停止旧 Ubuntu 环境"), "停止环境"},
	{messagePrefix("正在把旧 Ubuntu 环境原样移到"), "移走旧环境"},
	{messagePrefix("正在准备 "), "准备镜像"},
}

// messagePrefix marks the start of a progress message: matched against the
// message, never shown, so it needs no English.
func messagePrefix(text string) string { return text }

// jobStage is the status bar's name for the progress message of a job of
// kind.
func jobStage(kind, message string) string {
	message = strings.TrimPrefix(message, "设置已保存，")
	for _, entry := range jobStages {
		if strings.HasPrefix(message, entry.prefix) {
			return entry.stage
		}
	}
	return jobName(kind)
}

// finishedStage is what the status bar shows once a job has ended: nothing
// after success, the failure in short otherwise.
func finishedStage(job Job) string {
	switch {
	case job.Canceled:
		return "已取消"
	case job.Error == "":
		return ""
	case strings.Contains(job.Message, "联网检查未通过"):
		return "联网检查未通过"
	}
	return jobName(job.Kind) + "失败"
}
