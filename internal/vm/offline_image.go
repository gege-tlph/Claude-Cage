package vm

import (
	"context"
	"errors"
	"runtime"

	"github.com/lima-vm/lima/v2/pkg/limatmpl"
)

// PinnedDebianImage is the official location and SHA-512 of the Debian 13
// cloud image this version installs for the host's architecture: the first
// location cageDebianImages offers, which Lima tries first.
func (l *Lima) PinnedDebianImage(ctx context.Context) (string, string, error) {
	if _, err := l.prepare(); err != nil {
		return "", "", err
	}
	tmpl, err := limatmpl.Read(ctx, "", "template:debian-13")
	if err != nil {
		return "", "", err
	}
	if err := tmpl.Embed(ctx, true, false); err != nil {
		return "", "", err
	}
	if err := tmpl.Unmarshal(); err != nil {
		return "", "", err
	}
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	upstream := debianImageMirrors[0].base
	for _, img := range tmpl.Config.Images {
		if string(img.Arch) != arch || img.Variant != "genericcloud" || len(img.Location) <= len(upstream) || img.Location[:len(upstream)] != upstream {
			continue
		}
		if img.Digest.Validate() != nil || img.Digest.Algorithm() != "sha512" {
			continue
		}
		return img.Location, img.Digest.String(), nil
	}
	return "", "", errors.New("embedded Debian template contains no pinned cloud image")
}
