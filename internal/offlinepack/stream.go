package offlinepack

import (
	"archive/tar"
	_ "embed"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"time"
)

// WriteTar writes the guest files as a tar stream for GuestRoot, owned by
// root and readable by every account. progress gets the file bytes written.
func WriteTar(w io.Writer, dir string, m *Manifest, progress func(done, total int64)) error {
	tw := tar.NewWriter(w)
	total := m.TotalBytes()
	var done int64
	dirs := map[string]bool{}
	var names []string
	for _, f := range m.Files {
		for d := path.Dir(f.Path); d != "."; d = path.Dir(d) {
			if !dirs[d] {
				dirs[d] = true
				names = append(names, d)
			}
		}
	}
	sort.Strings(names)
	stamp := time.Unix(0, 0)
	for _, d := range names {
		if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: d + "/", Mode: 0o755, ModTime: stamp}); err != nil {
			return err
		}
	}
	buf := make([]byte, 1<<20)
	for _, f := range m.Files {
		in, err := os.Open(filepath.Join(dir, filepath.FromSlash(f.Path)))
		if err != nil {
			return err
		}
		err = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: f.Path, Mode: 0o644, Size: f.Size, ModTime: stamp})
		if err == nil {
			// Exactly the manifest's size: a file that grew since it was
			// checked fails here rather than shifting the stream.
			var n int64
			for n < f.Size && err == nil {
				var r int
				r, err = in.Read(buf[:min(int64(len(buf)), f.Size-n)])
				if r > 0 {
					if _, werr := tw.Write(buf[:r]); werr != nil {
						err = werr
					}
					n += int64(r)
					done += int64(r)
					if progress != nil {
						progress(done, total)
					}
				}
				if err == io.EOF && n == f.Size {
					err = nil
				}
			}
		}
		in.Close()
		if err != nil {
			return err
		}
	}
	return tw.Close()
}

// GuestPrepareScript runs as the guest user after the data was unpacked:
// APT takes only the copied repositories, the tool releases go where
// cage_install_tool looks for them.
//
//go:embed guest-prepare.sh
var GuestPrepareScript string

// FetchFunction defines cage_offline_fetch, which cage_curl_download tries
// first.
//
//go:embed guest-fetch.sh
var FetchFunction string

// GuestFinishScript runs at the end of every Cage setup, before the build is
// stopped: it removes the copied data and APT's offline configuration, and
// marks the installation as finished.
//
//go:embed guest-finish.sh
var GuestFinishScript string
