package usererror

import (
	"context"
	"crypto/x509"
	"errors"
	"io/fs"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

// A title that already names the outcome is not followed by another "失败".
func TestTitledKeepsTheTitleAndCause(t *testing.T) {
	cause := errors.New("eof")
	err := Titled("TLS 握手超时", "主检测服务：等待响应超时。", "外网保持关闭。", cause)
	if got := err.Error(); got != "TLS 握手超时：主检测服务：等待响应超时。外网保持关闭。" || !errors.Is(err, cause) {
		t.Fatalf("Titled = %q", got)
	}
	if got := New("连接代理", "超时", "重试。", nil).Error(); got != "连接代理失败：超时。重试。" {
		t.Fatalf("New = %q", got)
	}
}

func TestKnownFailureIsReadableAndPreservesCause(t *testing.T) {
	for _, tc := range []struct {
		cause  error
		reason string
	}{
		{context.DeadlineExceeded, "超时"},
		{&url.Error{Op: "GET", URL: "https://user:private@example.com/secret", Err: &net.DNSError{Err: "no such host", Name: "secret.example"}}, "域名"},
		{&fs.PathError{Op: "open", Path: "/private-secret/file", Err: fs.ErrPermission}, "拒绝访问"},
		{syscall.ENOSPC, "空间不足"},
		{syscall.ECONNREFUSED, "拒绝了连接"},
		{x509.UnknownAuthorityError{}, "证书"},
	} {
		err := Wrap("连接更新源", tc.cause, "请检查所选代理。")
		if !strings.Contains(err.Error(), tc.reason) || !strings.Contains(err.Error(), "连接更新源") || strings.Contains(err.Error(), "private") {
			t.Fatalf("unsafe or vague error: %v", err)
		}
		if !errors.Is(err, tc.cause) {
			t.Fatal("original error identity lost")
		}
	}
	precise := errors.New("上传窗口已关闭，请重新选择文件")
	if Display("上传", precise, "") != precise {
		t.Fatal("specific application message was replaced")
	}
	localized := &fs.PathError{Op: "open", Path: "/Users/测试用户/private-secret", Err: fs.ErrPermission}
	if message := Display("保存配置", localized, "请检查写入权限。").Error(); !strings.Contains(message, "拒绝访问") || strings.Contains(message, "private-secret") {
		t.Fatalf("localized system error bypassed safe explanation: %s", message)
	}
}
