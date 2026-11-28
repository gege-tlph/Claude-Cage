"""Run on Linux with permission to create a temporary network namespace."""
import os
from pathlib import Path
import select
import shutil
import socket
import subprocess
import sys
import tempfile
import unittest


@unittest.skipUnless(sys.platform == "linux" and os.geteuid() == 0 and shutil.which("ip"),
                     "requires Linux network namespaces and iproute2")
class AccountHostToolsTest(unittest.TestCase):
    def test_existing_account_reconnects_when_host_tools_restart(self):
        relay = Path(__file__).with_name("account-relay.py")
        with tempfile.TemporaryDirectory() as directory:
            endpoint = Path(directory) / "tools.sock"
            # The relay starts before host tools, in its own network namespace.
            command = ["unshare", "-n", "sh", "-ec", 'ip link set lo up; exec "$@"', "--",
                       sys.executable, str(relay), directory + "/unused-egress",
                       directory + "/browser.sock", str(endpoint), "18962"]
            with subprocess.Popen(command, stdin=subprocess.PIPE, stdout=subprocess.PIPE) as process:
                try:
                    self.assertTrue(select.select([process.stdout], [], [], 5)[0], "relay startup timed out")
                    self.assertTrue(process.stdout.readline().startswith(b"ready "))
                    client = ["nsenter", "-t", str(process.pid), "-n", sys.executable, "-c",
                              "import socket; s = socket.create_connection(('127.0.0.1', 18962), 3); "
                              "s.sendall(b'authenticated request'); print(s.recv(100).decode())"]
                    for response in (b"first server", b"restarted server"):
                        with socket.socket(socket.AF_UNIX) as server:
                            server.settimeout(3)
                            server.bind(str(endpoint))
                            server.listen()
                            with subprocess.Popen(client, stdout=subprocess.PIPE) as caller:
                                conn, _ = server.accept()
                                with conn, conn.makefile("rb") as request:
                                    self.assertEqual(request.read(len(b"authenticated request")),
                                                     b"authenticated request")
                                    conn.sendall(response)
                                self.assertEqual(caller.communicate(timeout=5)[0].strip(), response)
                        endpoint.unlink()
                finally:
                    process.stdin.close()
                    process.wait(timeout=5)


if __name__ == "__main__":
    unittest.main()
