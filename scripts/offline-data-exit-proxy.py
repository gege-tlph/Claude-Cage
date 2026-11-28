"""Proxy exit for building the full package's installation data in CI.

The installation that the data is collected from runs through a Cage proxy
node like any user's. This is that node: a SOCKS5 server (CONNECT only, no
authentication) that runs in its own container, so its address is not one
of the runner's and Cage takes it as a remote node. Every target is printed,
so the job log shows what the installation reached.

Run: python3 -I offline-data-exit-proxy.py --port 1080
"""
import argparse
import asyncio
import socket
import struct
import sys
import time


def log(*parts):
    print(time.strftime("%H:%M:%S"), *parts, flush=True)


async def pipe(src, dst, counter, index):
    try:
        while True:
            data = await src.read(65536)
            if not data:
                break
            counter[index] += len(data)
            dst.write(data)
            await dst.drain()
    except (ConnectionError, OSError):
        pass
    finally:
        try:
            dst.close()
        except Exception:
            pass


async def handle(reader, writer):
    try:
        head = await reader.readexactly(2)
        if head[0] != 5:
            return
        methods = await reader.readexactly(head[1])
        if 0 not in methods:
            writer.write(b"\x05\xff")
            await writer.drain()
            return
        writer.write(b"\x05\x00")
        await writer.drain()
        _, cmd, _, atyp = await reader.readexactly(4)
        if atyp == 1:
            host = socket.inet_ntoa(await reader.readexactly(4))
        elif atyp == 3:
            size = (await reader.readexactly(1))[0]
            host = (await reader.readexactly(size)).decode("ascii", "replace")
        elif atyp == 4:
            host = socket.inet_ntop(socket.AF_INET6, await reader.readexactly(16))
        else:
            return
        port = struct.unpack("!H", await reader.readexactly(2))[0]
        if cmd != 1:
            writer.write(b"\x05\x07\x00\x01" + b"\x00" * 6)
            await writer.drain()
            return
        started = time.monotonic()
        try:
            up_reader, up_writer = await asyncio.wait_for(asyncio.open_connection(host, port), 30)
        except Exception as exc:
            log("fail", f"{host}:{port}", type(exc).__name__)
            writer.write(b"\x05\x04\x00\x01" + b"\x00" * 6)
            await writer.drain()
            return
        writer.write(b"\x05\x00\x00\x01" + b"\x00" * 6)
        await writer.drain()
        counter = [0, 0]
        await asyncio.gather(pipe(reader, up_writer, counter, 0), pipe(up_reader, writer, counter, 1))
        log("done", f"{host}:{port}", f"up={counter[0]}", f"down={counter[1]}", f"ms={int((time.monotonic() - started) * 1000)}")
    except (asyncio.IncompleteReadError, ConnectionError, OSError):
        pass
    finally:
        try:
            writer.close()
        except Exception:
            pass


async def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=1080)
    args = parser.parse_args()
    server = await asyncio.start_server(handle, "0.0.0.0", args.port)
    log("listening", args.port)
    async with server:
        await server.serve_forever()


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
