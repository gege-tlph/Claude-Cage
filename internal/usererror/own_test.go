package usererror

import (
	"errors"
	"testing"
)

func TestOwnCopiesAFailureAndItsTools(t *testing.T) {
	cause := errors.New("cause")
	shared := Because("原因是找不到可用的 IPv4 出口网卡", "重试", cause, ToolReapply)
	own, ok := AsFailure(Own(shared))
	if !ok || own == shared {
		t.Fatalf("not copied: %v", own)
	}
	own.Lead, own.Tools[0] = "启动环境失败", ToolStart
	if shared.Lead != "" || shared.Tools[0] != ToolReapply {
		t.Fatalf("the shared failure changed: %+v", shared)
	}
	if !errors.Is(own, cause) {
		t.Fatal("the copy lost its cause")
	}
	wrapped := errors.Join(errors.New("wrapper"), shared)
	if Own(wrapped) != wrapped || Own(nil) != nil {
		t.Fatal("a wrapper or nil must come back as it is")
	}
}
