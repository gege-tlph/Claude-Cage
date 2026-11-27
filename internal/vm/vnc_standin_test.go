package vm

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/lima-vm/lima/v2/pkg/portfwd"

	"github.com/sylvinus/agent-vm/internal/host"
)

// standInEnv makes the test binary a stand-in for one of a VM's processes
// instead of running the tests (TestMain): "hostagent" or "qemu", which
// live until their stdin closes, or "exit", which exits at once.
const standInEnv = "CAGE_VM_TEST_STAND_IN"

const (
	// standInListen is the address a hostagent's display forward holds, bound
	// as Lima's forwarder binds it.
	standInListen = "CAGE_VM_TEST_LISTEN"
	// standInSocket is the hostagent API socket the stand-in serves /v1/info
	// on, standInSocketDelay how long it takes to answer.
	standInSocket      = "CAGE_VM_TEST_SOCKET"
	standInSocketDelay = "CAGE_VM_TEST_SOCKET_DELAY"
	// standInStopDir makes the hostagent answer Cage's stop request for that
	// instance as the real one does: its QEMU (standInQEMU, a PID) goes,
	// then itself.
	standInStopDir = "CAGE_VM_TEST_STOP_DIR"
	standInQEMU    = "CAGE_VM_TEST_QEMU_PID"
)

func runStandIn(role string) {
	if role == "exit" {
		return
	}
	if address := os.Getenv(standInListen); address != "" {
		listener, err := (&net.ListenConfig{Control: portfwd.Control}).Listen(context.Background(), "tcp", address)
		if err != nil {
			fmt.Println("listen:", err)
			os.Exit(2)
		}
		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				conn.Close()
			}
		}()
	}
	if socket := os.Getenv(standInSocket); socket != "" {
		delay, _ := time.ParseDuration(os.Getenv(standInSocketDelay))
		listener, err := net.Listen("unix", socket)
		if err != nil {
			fmt.Println("socket:", err)
			os.Exit(2)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/v1/info", func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(delay)
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"sshLocalPort": 60022}`)
		})
		go http.Serve(listener, mux)
	}
	if dir := os.Getenv(standInStopDir); dir != "" {
		stop, err := host.ListenInstanceStop(dir, func() {
			if pid, err := strconv.Atoi(os.Getenv(standInQEMU)); err == nil {
				if p, err := os.FindProcess(pid); err == nil {
					p.Kill()
				}
			}
			os.Exit(0)
		})
		if err != nil {
			fmt.Println("stop event:", err)
			os.Exit(2)
		}
		defer stop()
	}
	fmt.Println("ready")
	// Until the test is done with it, or gone.
	io.Copy(io.Discard, os.Stdin)
}
