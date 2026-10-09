#!/usr/bin/env python3
"""
Stand-in for the wocam smart-streamer "initiate" endpoint.

On "start" it begins publishing a test pattern into MediaMTX under the same
path the request named; on "stop" it kills that publisher. This lets the
on-demand HTTP hook be exercised fully offline, with no real camera or
staging API involved.
"""
import json
import http.server
import subprocess
import threading

MEDIAMTX_HOST = "mediamtx"  # service name on the compose network

procs = {}
procs_lock = threading.Lock()


class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(length) or b"{}")

        camera = body.get("camera", "unknown")
        action = body.get("type", "")
        api_key = self.headers.get("x-api-key", "")

        print(f"[mock-api] {action} camera={camera} key={api_key!r}", flush=True)

        if action == "start":
            self._start(camera)
        elif action == "stop":
            self._stop(camera)

        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"ok":true}')

    def _start(self, camera):
        with procs_lock:
            if camera in procs:
                return
            # path name convention used by mediamtx.yml: <anything>-<anything>/<camera>
            # the test client is expected to request e.g. acme-nyc/<camera>
            url = f"rtsp://{MEDIAMTX_HOST}:8554/acme-nyc/{camera}"
            procs[camera] = subprocess.Popen([
                "ffmpeg", "-nostdin", "-re",
                "-f", "lavfi", "-i", f"testsrc=size=320x240:rate=15",
                "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency",
                "-f", "rtsp", url,
            ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            print(f"[mock-api] started publishing {url}", flush=True)

    def _stop(self, camera):
        with procs_lock:
            p = procs.pop(camera, None)
        if p is not None:
            p.kill()
            print(f"[mock-api] stopped publishing camera={camera}", flush=True)

    def log_message(self, *args):
        pass  # quiet the default access log; we print our own lines above


if __name__ == "__main__":
    http.server.HTTPServer(("0.0.0.0", 9999), Handler).serve_forever()
