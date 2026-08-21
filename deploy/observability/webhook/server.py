import json
from http.server import BaseHTTPRequestHandler, HTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"status":"ok"}\n')

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length)
        print(json.dumps({"received": len(body)}), flush=True)
        self.send_response(202)
        self.end_headers()

    def log_message(self, *_args):
        return


HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
