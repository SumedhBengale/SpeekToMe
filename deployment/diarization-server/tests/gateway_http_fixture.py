"""Serve production gateway handlers with synthetic inference for Go contract tests.

Uses only the standard library. No models, production credentials or cloud calls.
"""
import ast
import asyncio
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import logging
import os
from pathlib import Path
import secrets
import shutil
import sys
import threading
import time


class HTTPException(Exception):
    def __init__(self, status_code, detail):
        self.status_code = status_code
        super().__init__(detail)


class Response:
    def __init__(self, content, status_code=200, headers=None):
        self.content, self.status_code, self.headers = content, status_code, headers or {}


class FileResponse:
    def __init__(self, path, media_type):
        self.path = path


class Request:
    def __init__(self, handler):
        self.handler, self.headers = handler, handler.headers

    async def stream(self):
        yield self.handler.rfile.read(int(self.headers['content-length']))


def main():
    data = Path(sys.argv[1])
    data.mkdir(exist_ok=True)
    ns = dict(asyncio=asyncio, Path=Path, threading=threading, os=os, json=json,
              time=time, secrets=secrets, shutil=shutil, HTTPException=HTTPException,
              JSONResponse=Response, FileResponse=FileResponse, Request=Request,
              DATA=data, ready=True, active=None, lock=threading.RLock(),
              slot=threading.Lock(), MAX_UPLOAD=1024, RESERVE=0,
              log=logging.getLogger('fixture'), cleanup_old=lambda: None)
    ns['snapshot'] = lambda: {'active_job': ns['active']}

    def infer(job):
        # Only inference is synthetic; admission, persistence, status, result and
        # acknowledgment are executed from the production Python source.
        with ns['lock']:
            count = data/'inference-count'
            count.write_text(str(int(count.read_text()) + 1 if count.exists() else 1))
            (data/job['job_id']/'result.json').write_text(json.dumps({
                'segments': [{'Start': 0, 'End': 1, 'Speaker': 0}]
            }))
            ns['update'](job, persist=True, status='completed', finished_at=time.time())
            ns['active'] = None
            ns['slot'].release()

    ns['run_job'] = infer
    source = Path(__file__).resolve().parents[1]/'gateway.py'
    tree = ast.parse(source.read_text())
    functions = []
    for node in tree.body:
        if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and node.name in (
                'atomic_json', 'job_dir', 'update', 'public_job', 'submit',
                'status', 'result', 'acknowledge'):
            node.decorator_list = []
            functions.append(node)
    exec(compile(ast.Module(body=functions, type_ignores=[]), str(source), 'exec'), ns)

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def handle_request(self):
            if self.headers.get('authorization') != 'Bearer fixture-only-token-0123456789abcdef':
                self.send_error(401)
                return
            try:
                path = self.path.split('?')[0]
                if self.command == 'POST' and path == '/v1/diarizations':
                    response = asyncio.run(ns['submit'](Request(self)))
                elif path.startswith('/v1/diarizations/'):
                    identity = path.split('/')[3]
                    function = ('acknowledge' if self.command == 'DELETE' else
                                'result' if path.endswith('/result') else 'status')
                    response = asyncio.run(ns[function](identity))
                else:
                    raise HTTPException(404, 'Unknown path')
                if isinstance(response, FileResponse):
                    body, code, headers = response.path.read_bytes(), 200, {}
                else:
                    response = response if isinstance(response, Response) else Response(response)
                    body, code, headers = json.dumps(response.content).encode(), response.status_code, response.headers
            except HTTPException as error:
                body, code, headers = json.dumps({'error': str(error)}).encode(), error.status_code, {}
            self.send_response(code)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(body)))
            for key, value in headers.items():
                self.send_header(key, value)
            self.end_headers()
            self.wfile.write(body)

        do_POST = do_GET = do_DELETE = handle_request

    server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    print(f'http://127.0.0.1:{server.server_port}', flush=True)
    server.serve_forever()


if __name__ == '__main__':
    main()
