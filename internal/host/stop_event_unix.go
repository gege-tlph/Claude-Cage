//go:build !windows

package host

func ListenInstanceStop(string, func()) (func(), error) { return func() {}, nil }
func RequestInstanceStop(string) (bool, error)          { return false, nil }

// InstanceHostagentRunning has nothing to go by besides Lima's status here.
func InstanceHostagentRunning(string) bool { return false }
