#!/usr/bin/env python3
"""Minimal chat UI that proxies to a vLLM OpenAI endpoint.

Server-side proxy avoids browser CORS: the page calls /api/chat on this server,
which forwards to ${VLLM_BASE_URL}/v1/chat/completions with model=${MODEL}.
Renders the resolved wiring so the formae graph is visible on screen.
"""
import json
import os
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

VLLM_BASE_URL = os.environ.get("VLLM_BASE_URL", "http://localhost:8000").rstrip("/")
MODEL = os.environ.get("MODEL", "")
PORT = int(os.environ.get("PORT", "8080"))

PAGE = """<!doctype html><html><head><meta charset="utf-8"><title>formae chat</title>
<style>
 body{{font:16px system-ui;margin:0;background:#0b0f17;color:#e6edf3}}
 header{{padding:12px 16px;background:#11161f;border-bottom:1px solid #222}}
 .wire{{font:12px monospace;color:#8b949e}} .wire b{{color:#58a6ff}}
 #log{{padding:16px;max-width:760px;margin:0 auto}}
 .msg{{margin:8px 0;padding:10px 12px;border-radius:8px;white-space:pre-wrap}}
 .u{{background:#1f2937}} .a{{background:#162447}}
 form{{display:flex;gap:8px;max-width:760px;margin:0 auto;padding:16px}}
 input{{flex:1;padding:10px;border-radius:8px;border:1px solid #333;background:#0d1117;color:#e6edf3}}
 button{{padding:10px 16px;border:0;border-radius:8px;background:#238636;color:#fff;cursor:pointer}}
</style></head><body>
<header><b>formae &middot; vLLM chat</b>
 <div class="wire">model <b>{model}</b> @ <b>{base}</b> &mdash; wired by formae resolvables</div>
</header>
<div id="log"></div>
<form id="f"><input id="m" autocomplete="off" placeholder="Say something..."><button>Send</button></form>
<script>
const log=document.getElementById('log'),f=document.getElementById('f'),m=document.getElementById('m');
function add(t,c){{const d=document.createElement('div');d.className='msg '+c;d.textContent=t;log.appendChild(d);d.scrollIntoView();}}
f.onsubmit=async e=>{{e.preventDefault();const q=m.value.trim();if(!q)return;add(q,'u');m.value='';
 try{{const r=await fetch('/api/chat',{{method:'POST',headers:{{'content-type':'application/json'}},body:JSON.stringify({{message:q}})}});
 const j=await r.json();add(j.reply||j.error||'(no reply)','a');}}catch(err){{add('error: '+err,'a');}}}};
</script></body></html>"""


class Handler(BaseHTTPRequestHandler):
    def _send(self, code, body, ctype="application/json"):
        data = body if isinstance(body, bytes) else body.encode()
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path in ("/", "/index.html"):
            self._send(200, PAGE.format(model=MODEL, base=VLLM_BASE_URL), "text/html")
        elif self.path == "/healthz":
            self._send(200, b'{"ok":true}')
        else:
            self._send(404, b'{"error":"not found"}')

    def do_POST(self):
        if self.path != "/api/chat":
            self._send(404, b'{"error":"not found"}')
            return
        n = int(self.headers.get("Content-Length", "0"))
        try:
            msg = json.loads(self.rfile.read(n) or b"{}").get("message", "")
            payload = json.dumps({
                "model": MODEL,
                "messages": [{"role": "user", "content": msg}],
                "max_tokens": 256,
                "temperature": 0.7,
            }).encode()
            req = urllib.request.Request(
                VLLM_BASE_URL + "/v1/chat/completions",
                data=payload, headers={"Content-Type": "application/json"},
            )
            with urllib.request.urlopen(req, timeout=60) as resp:
                body = json.loads(resp.read())
            reply = body["choices"][0]["message"]["content"]
            self._send(200, json.dumps({"reply": reply}))
        except Exception as e:  # noqa: BLE001 — surface any upstream error to the page
            self._send(502, json.dumps({"error": str(e)}))

    def log_message(self, *a):  # quiet
        pass


if __name__ == "__main__":
    print(f"chat-ui on :{PORT} -> model={MODEL!r} @ {VLLM_BASE_URL}", flush=True)
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
