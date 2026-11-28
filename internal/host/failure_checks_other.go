//go:build !windows

package host

func unsupportedVolume(string) string { return "" }
