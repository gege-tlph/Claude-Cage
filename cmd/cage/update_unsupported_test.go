//go:build !linux

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sylvinus/agent-vm/internal/appupdate"
)

func TestHostUpdateInstallPointsToTheApp(t *testing.T) {
	err := installHostUpdate(context.Background(), "", appupdate.State{}, nil, nil, nil, true)
	if err == nil || !strings.Contains(err.Error(), "设置 > 关于") {
		t.Fatalf("err = %v", err)
	}
}
