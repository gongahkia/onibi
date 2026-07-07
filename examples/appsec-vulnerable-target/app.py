from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/":
            self.respond(
                200,
                "text/html",
                b"<h1>KelpClaw AppSec fixture</h1><a href='/debug/env'>debug</a>",
            )
            return
        if self.path == "/debug/env":
            self.respond(
                200,
                "application/json",
                json.dumps(
                    {
                        "debug": True,
                        "environment": "fixture",
                        "sample_secret": "not-a-real-secret",
                    },
                    sort_keys=True,
                ).encode(),
            )
            return
        if self.path == "/admin":
            self.respond(200, "text/plain", b"default admin marker: admin/admin")
            return
        self.respond(404, "text/plain", b"not found")

    def log_message(self, format, *args):
        return

    def respond(self, status, content_type, body):
        self.send_response(status)
        self.send_header("content-type", content_type)
        self.send_header("x-fixture-appsec", "enabled")
        self.end_headers()
        self.wfile.write(body)


if __name__ == "__main__":
    port = int(os.environ.get("PORT", "8080"))
    ThreadingHTTPServer(("0.0.0.0", port), Handler).serve_forever()
