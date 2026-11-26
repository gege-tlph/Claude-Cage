package appupdate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/sylvinus/agent-vm/internal/appupdate/catalog"
	"github.com/sylvinus/agent-vm/internal/version"
)

func aboutClient(t *testing.T, handle func(*http.Request) (int, any)) *http.Client {
	t.Helper()
	return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.UserAgent(), "Cage") {
			t.Errorf("product identity in user agent: %q", r.UserAgent())
		}
		status, value := handle(r)
		b, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(b))), ContentLength: int64(len(b)), Request: r}, nil
	})}
}

// A prerelease build reads the API's full list, prereleases included; a
// release build reads the latest release's manifest.
func TestAboutReleaseIncludesPrereleasesWithoutRequiringPlatformAssets(t *testing.T) {
	for _, current := range []struct{ version, status string }{{"0.3.0-rc.9", "available"}, {"0.3.0-rc.10", "current"}, {"0.3.0-rc.11", "ahead"}} {
		t.Run(current.version, func(t *testing.T) {
			client := aboutClient(t, func(r *http.Request) (int, any) {
				if r.URL.Host != "api.github.com" {
					t.Errorf("a prerelease build read %s", r.URL)
				}
				return 200, []release{{Tag: "v0.2.0"}, {Tag: "v0.3.0-rc.9", Prerelease: true}, {Tag: "v0.3.0-rc.10", Prerelease: true}, {Tag: "v0.4.0", Draft: true}, {Tag: "not-a-version"}}
			})
			got := checkPublishedRelease(context.Background(), client, current.version)
			if got.Status != current.status || got.Version != "0.3.0-rc.10" || !got.Prerelease {
				t.Fatalf("%+v", got)
			}
		})
	}
	for _, current := range []struct{ version, status string }{{"0.2.2", "available"}, {"0.2.3", "current"}, {"0.2.4", "ahead"}, {"development", "unknown"}} {
		t.Run(current.version, func(t *testing.T) {
			client := aboutClient(t, func(r *http.Request) (int, any) {
				if r.URL.String() != catalog.LatestManifestURL {
					t.Errorf("a release build read %s", r.URL)
				}
				return 200, release{Tag: "v0.2.3"}
			})
			got := checkPublishedRelease(context.Background(), client, current.version)
			if got.Status != current.status || got.Version != "0.2.3" || got.Prerelease {
				t.Fatalf("%+v", got)
			}
		})
	}
	if got := checkPublishedRelease(context.Background(), responseClient(`[{"tag_name":"v0.1.0","draft":true}]`, 200), "0.1.0-rc.1"); got.Status != "missing" {
		t.Fatalf("%+v", got)
	}
}

// The latest release before 0.2.3 has no manifest: the API answers instead.
// A failure other than a missing manifest is reported, not hidden by the API.
func TestAboutManifestFallsBackOnlyWhenMissing(t *testing.T) {
	requests := []string{}
	client := aboutClient(t, func(r *http.Request) (int, any) {
		requests = append(requests, r.URL.Host)
		if r.URL.Host == "github.com" {
			return 404, map[string]string{"message": "Not Found"}
		}
		return 200, []release{{Tag: "v0.2.2"}}
	})
	if got := checkPublishedRelease(context.Background(), client, "0.2.1"); got.Status != "available" || got.Version != "0.2.2" || strings.Join(requests, ",") != "github.com,api.github.com" {
		t.Fatalf("%+v %v", got, requests)
	}
	requests = nil
	client = aboutClient(t, func(r *http.Request) (int, any) {
		requests = append(requests, r.URL.Host)
		return 503, nil
	})
	if got := checkPublishedRelease(context.Background(), client, "0.2.1"); got.Status != "failed" || strings.Join(requests, ",") != "github.com" {
		t.Fatalf("%+v %v", got, requests)
	}
	// A manifest that does not read as a release is not trusted as one.
	requests = nil
	client = aboutClient(t, func(r *http.Request) (int, any) {
		requests = append(requests, r.URL.Host)
		if r.URL.Host == "github.com" {
			return 200, []string{"not", "a", "release"}
		}
		return 200, []release{{Tag: "v0.2.2"}}
	})
	if got := checkPublishedRelease(context.Background(), client, "0.2.1"); got.Version != "0.2.2" || len(requests) != 2 {
		t.Fatalf("%+v %v", got, requests)
	}
}

func TestAboutChecksPreservePartialSuccess(t *testing.T) {
	client := aboutClient(t, func(r *http.Request) (int, any) { return 429, nil })
	got := checkAbout(context.Background(), client, version.Build{Version: "0.1.0"})
	if got.Release.Status != "failed" || got.Release.RetryAt == "" || got.CheckedAt == "" {
		t.Fatalf("%+v", got)
	}
}

func TestAboutMetadataRejectsUntrustedPaths(t *testing.T) {
	for _, path := range []string{"commits/main", "commits/other", "compare/" + strings.Repeat("a", 40) + "..." + strings.Repeat("b", 40), "issues", "../Other/releases"} {
		u, _ := url.Parse(repositoryAPI + "/" + path)
		if allowedURL(u, false) {
			t.Fatalf("accepted %s", u)
		}
	}
	for _, raw := range []string{"https://github.com/" + repository + "/releases/latest/download/other.json", "https://github.com/" + repository + "/releases/latest", "https://github.com/Other/Claude-Cage/releases/latest/download/" + catalog.ManifestName} {
		u, _ := url.Parse(raw)
		if allowedURL(u, false) {
			t.Fatalf("accepted %s", u)
		}
	}
	if u, _ := url.Parse(catalog.LatestManifestURL); !allowedURL(u, false) {
		t.Fatal("the manifest is not a trusted update source")
	}
}
