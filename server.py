#!/usr/bin/env python3
"""HTTP server with retry on port bind."""

import http.server
import socket
import signal
import sys
import time

PORT = 8080
MAX_RETRIES = 10
RETRY_DELAY = 1

def handler(sig, frame):
    print(f"[server] signal {sig}, exiting")
    sys.exit(0)

signal.signal(signal.SIGTERM, handler)
signal.signal(signal.SIGINT, handler)

# Create socket manually with SO_REUSEADDR and SO_REUSEPORT
for attempt in range(MAX_RETRIES):
    try:
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        sock.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEPORT, 1)
        sock.bind(('', PORT))
        sock.listen(5)
        print(f"[server] bound to port {PORT}")
        break
    except OSError as e:
        print(f"[server] bind failed ({e}), retry {attempt+1}/{MAX_RETRIES}")
        time.sleep(RETRY_DELAY)
else:
    print("[server] failed to bind after all retries")
    sys.exit(1)

# Serve using the pre-bound socket
class Handler(http.server.SimpleHTTPRequestHandler):
    pass

with http.server.HTTPServer(('', PORT), Handler, bind_and_activate=False) as httpd:
    httpd.socket = sock
    print(f"[server] serving on port {PORT}")
    httpd.serve_forever()
