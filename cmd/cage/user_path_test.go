package main

import (
	"strings"
	"testing"
)

func TestUserPathEntries(t *testing.T) {
	const dir = `C:\Users\me\AppData\Local\Programs\Cage\resources`
	expand := func(s string) string {
		return strings.ReplaceAll(s, "%LOCALAPPDATA%", `C:\Users\me\AppData\Local`)
	}
	for _, c := range []struct{ path, added string }{
		{"", dir},
		{`C:\bin`, `C:\bin;` + dir},
		{`C:\bin;`, `C:\bin;` + dir},
		{`%USERPROFILE%\go\bin;;C:\bin`, `%USERPROFILE%\go\bin;;C:\bin;` + dir},
	} {
		got, changed := withPathEntry(c.path, dir, expand)
		if got != c.added || !changed {
			t.Errorf("add to %q: %q %v", c.path, got, changed)
		}
		back, removed := withoutPathEntry(got, dir, expand)
		if !removed || strings.Contains(strings.ToLower(back), `cage\resources`) {
			t.Errorf("remove from %q: %q %v", got, back, removed)
		}
	}
	// Already listed in another spelling: nothing changes.
	for _, spelling := range []string{
		strings.ToUpper(dir),
		dir + `\`,
		`"` + dir + `"`,
		`%LOCALAPPDATA%\Programs\Cage\resources`,
	} {
		path := `C:\bin;` + spelling + `;D:\tools`
		if got, changed := withPathEntry(path, dir, expand); changed || got != path {
			t.Errorf("added a duplicate of %q: %q", spelling, got)
		}
		if got, changed := withoutPathEntry(path, dir, expand); !changed || got != `C:\bin;D:\tools` {
			t.Errorf("remove %q: %q", spelling, got)
		}
	}
	// Other entries, including a parent or a sibling, stay untouched.
	path := `C:\Users\me\AppData\Local\Programs\Cage;C:\Users\me\AppData\Local\Programs\Cage\resources2`
	if got, changed := withoutPathEntry(path, dir, expand); changed || got != path {
		t.Errorf("removed an unrelated entry: %q", got)
	}
}
