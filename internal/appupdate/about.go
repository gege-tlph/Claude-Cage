package appupdate

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/sylvinus/agent-vm/internal/version"
	"golang.org/x/mod/semver"
)

const repositoryAPI = "https://api.github.com/repos/" + repository

// CheckItem keeps a failure visible with its retry time.
type CheckItem struct {
	Status     string `json:"status"`
	Version    string `json:"version,omitempty"`
	Prerelease bool   `json:"prerelease,omitempty"`
	Error      string `json:"error,omitempty"`
	RetryAt    string `json:"retryAt,omitempty"`
}

// AboutCheck no longer compares the build with the main branch: that took two
// more API requests per check and told users nothing they could act on.
type AboutCheck struct {
	Build     version.Build `json:"build"`
	Release   CheckItem     `json:"release"`
	CheckedAt string        `json:"checkedAt"`
}

// CheckAbout only reads metadata. It never downloads, installs, or changes the
// native updater's stable/prerelease channel.
func CheckAbout(ctx context.Context, configPath string) AboutCheck {
	ctx, cancel := context.WithTimeout(ctx, 75*time.Second)
	defer cancel()
	b := version.CurrentBuild()
	client := updateClient(configPath)
	defer client.CloseIdleConnections()
	return checkAbout(ctx, client, b)
}

func checkAbout(ctx context.Context, client *http.Client, b version.Build) AboutCheck {
	result := AboutCheck{Build: b, Release: checkPublishedRelease(ctx, client, b.Version)}
	result.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	return result
}

func checkFailure(err error) CheckItem {
	item := CheckItem{Status: "failed", Error: err.Error()}
	var delayed interface{ RetryAt() time.Time }
	if errors.As(err, &delayed) {
		item.RetryAt = delayed.RetryAt().UTC().Format(time.RFC3339)
	}
	return item
}

func checkPublishedRelease(ctx context.Context, client *http.Client, current string) CheckItem {
	releases, err := publishedReleases(ctx, client, current)
	if err != nil {
		return checkFailure(err)
	}
	item := CheckItem{Status: "missing"}
	for _, r := range releases {
		if r.Draft || !semver.IsValid(r.Tag) || semver.Canonical(r.Tag) != r.Tag {
			continue
		}
		if item.Version != "" && semver.Compare(r.Tag, "v"+item.Version) <= 0 {
			continue
		}
		item.Version, item.Prerelease = strings.TrimPrefix(r.Tag, "v"), r.Prerelease || semver.Prerelease(r.Tag) != ""
	}
	if item.Version == "" {
		return item
	}
	item.Status = "unknown"
	if tag := "v" + current; semver.IsValid(tag) && semver.Canonical(tag) == tag {
		switch semver.Compare("v"+item.Version, tag) {
		case 1:
			item.Status = "available"
		case 0:
			item.Status = "current"
		case -1:
			item.Status = "ahead"
		}
	}
	return item
}
