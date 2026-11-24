package workbench

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// #846: a storage that cannot be recovered or read left no way out: retries
// fail, every environment page is locked and the rebuild entry is hidden.
// Rebuilding moves the broken storage aside, never deleting it, and lets the
// environment be prepared again at the default location.
func TestStorageResetLeavesUnrecoverableStorage(t *testing.T) {
	s := storageRecoveryServer(t)
	journal := storageRecoveryJournal(t, s)
	old := filepath.Join(s.profile, "state", "lima", "old", "lima.yaml")
	if err := os.MkdirAll(filepath.Dir(old), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("old environment"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.failStorageRecovery(errors.New("synthetic unrecoverable journal"))

	// Retrying cannot finish a journal only a removed guest runtime could.
	if response := desktopRequestForTest(s, http.MethodPost, "/api/environment/storage/recover", []byte(`{}`)); response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	if job := awaitJobIdle(t, &s.jobs); job.Error == "" || !s.storageRecoveryStatus().Required {
		t.Fatal("fixture journal unexpectedly recovered", job)
	}

	// Rebuilding is local and needs the trusted window.
	untrusted := httptest.NewRequest(http.MethodPost, s.origin+"/api/environment/storage/reset", strings.NewReader(`{}`))
	untrusted.Header.Set("Origin", s.origin)
	untrusted.Header.Set("Content-Type", "application/json")
	untrusted.AddCookie(&http.Cookie{Name: s.cookieName(), Value: s.session})
	if response := httptest.NewRecorder(); func() int { s.handler().ServeHTTP(response, untrusted); return response.Code }() != http.StatusForbidden {
		t.Fatal("untrusted view reset storage")
	}
	response := desktopRequestForTest(s, http.MethodPost, "/api/environment/storage/reset", []byte(`{}`))
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	if job := awaitJobIdle(t, &s.jobs); job.Error != "" || job.Kind != "storage-recovery" {
		t.Fatal("reset failed", job)
	}
	if s.storageRecoveryStatus().Required {
		t.Fatal("reset left the recovery gate closed")
	}
	if _, err := os.Stat(journal); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("reset left the unrecoverable journal in place", err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("old environment still in use", err)
	}
	var location struct {
		Directory string `json:"directory"`
		Reset     string `json:"reset"`
	}
	response = desktopRequestForTest(s, http.MethodGet, "/api/environment/storage", nil)
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &location) != nil || location.Directory == "" || location.Reset == "" {
		t.Fatal("storage after reset", response.Code, response.Body.String())
	}
	kept, err := os.ReadFile(filepath.Join(location.Reset, "state", "lima", "old", "lima.yaml"))
	if err != nil || string(kept) != "old environment" {
		t.Fatal("old environment was not kept in the archive", err)
	}
	if _, err := os.Stat(filepath.Join(location.Reset, "environment-storage-pending.json")); err != nil {
		t.Fatal("journal was not kept in the archive", err)
	}
	// Usable storage is never reset.
	if response := desktopRequestForTest(s, http.MethodPost, "/api/environment/storage/reset", []byte(`{}`)); response.Code != http.StatusConflict {
		t.Fatal("usable storage was reset", response.Code)
	}
}

// A custom location whose files no longer match its record is neither missing
// (it still holds files) nor recoverable: the settings page could not read it
// and offered nothing. It now names the cause and can be rebuilt.
func TestStorageResetForUnavailableLocation(t *testing.T) {
	s := storageRecoveryServer(t)
	custom := filepath.Join(t.TempDir(), "CageData")
	if err := os.MkdirAll(filepath.Join(custom, "state", "lima"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(custom, "state", "lima", "leftover"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writePrivate(filepath.Join(s.profile, "environment-storage.json"), map[string]string{"directory": custom, "id": strings.Repeat("b", 32)}); err != nil {
		t.Fatal(err)
	}
	response := desktopRequestForTest(s, http.MethodGet, "/api/environment/storage", nil)
	var view struct {
		Unavailable *struct {
			Error string `json:"error"`
		} `json:"unavailable"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &view) != nil || view.Unavailable == nil || view.Unavailable.Error == "" {
		t.Fatal("unavailable storage not reported", response.Code, response.Body.String())
	}
	if response := desktopRequestForTest(s, http.MethodPost, "/api/environment/storage/reset", []byte(`{}`)); response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	if job := awaitJobIdle(t, &s.jobs); job.Error != "" {
		t.Fatal("reset failed", job)
	}
	if response := desktopRequestForTest(s, http.MethodGet, "/api/environment/storage", nil); response.Code != http.StatusOK || strings.Contains(response.Body.String(), "unavailable") {
		t.Fatal("storage still unavailable after reset", response.Code, response.Body.String())
	}
	// The custom directory itself is left untouched.
	if _, err := os.Stat(filepath.Join(custom, "state", "lima", "leftover")); err != nil {
		t.Fatal("reset touched the custom directory", err)
	}
}
