// Command cage-offline-data builds the installation data of the full package
// (internal/offlinepack) from an environment that was just installed online
// by the same build, on a separate test profile:
//
//	cage-offline-data collect -cage <app>\resources\cage.exe -config <profile>\config.json -out collected.tar
//	cage-offline-data assemble -collected collected.tar -image-entry <storage>\cache\lima\download\by-url-sha256\<key> -version X.Y.Z -out offline
//
// collect runs collect.sh in the environment. assemble turns APT's signed
// indexes and archives into local repositories (the archives at the paths
// the indexes give them), keeps the tool releases, the NodeSource key and
// code-server's schemas, copies the Debian image and writes the manifest.
// It refuses an archive no captured index lists, and tool releases whose
// checksum is not their directory's name.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

//go:embed collect.sh
var collectScript string

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: cage-offline-data collect|assemble ...")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "collect":
		err = collect(os.Args[2:])
	case "assemble":
		err = assembleCommand(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func collect(args []string) error {
	fs := flag.NewFlagSet("collect", flag.ExitOnError)
	cage := fs.String("cage", "", "the build's cage.exe")
	config := fs.String("config", "", "the test profile's config.json")
	out := fs.String("out", "", "tar file to write")
	_ = fs.Parse(args)
	if *cage == "" || *config == "" || *out == "" {
		return fmt.Errorf("collect needs -cage, -config and -out")
	}
	f, err := os.Create(*out)
	if err != nil {
		return err
	}
	cmd := exec.Command(*cage, "run", "bash", "-s")
	cmd.Env = append(os.Environ(), "CLAUDE_CAGE_CONFIG="+*config)
	cmd.Stdin = strings.NewReader(collectScript)
	cmd.Stdout = f
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func assembleCommand(args []string) error {
	fs := flag.NewFlagSet("assemble", flag.ExitOnError)
	collected := fs.String("collected", "", "tar written by collect")
	imageEntry := fs.String("image-entry", "", "Lima cache entry of the Debian image (data, url, sha512.digest)")
	version := fs.String("version", "", "the app version the data is for")
	toolVersions := fs.String("tool-versions", "internal/cage/tool-versions.env", "the build's tool-versions.env")
	out := fs.String("out", "", "empty directory to write the data to")
	_ = fs.Parse(args)
	if *collected == "" || *imageEntry == "" || *version == "" || *out == "" {
		return fmt.Errorf("assemble needs -collected, -image-entry, -version and -out")
	}
	return assemble(*collected, *imageEntry, *version, *toolVersions, *out, os.Stdout)
}
