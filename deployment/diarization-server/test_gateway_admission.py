"""Test the gateway's actual admission/upload code without loading ONNX weights."""
import ast
import asyncio
import json
import struct
import logging
from pathlib import Path
import secrets
import shutil
import tempfile
import threading
import time
import unittest
import os


class Response:
    def __init__(self, content, status_code=200, headers=None):
        self.content, self.status_code = content, status_code


class PassthroughCipher:
    """Stands in for AES-GCM so fixtures need only the standard library."""

    def encrypt(self, nonce, data, aad):
        return bytes(data) + bytes(16)

    def decrypt(self, nonce, data, aad):
        return data[:-16]


class HTTPException(Exception):
    def __init__(self, status_code, detail):
        self.status_code = status_code
        super().__init__(detail)


class Request:
    def __init__(self, identity, fail=False):
        self.headers = {'x-speektome-job-id': identity, 'content-length': '5'}
        self.consumed = False
        self.fail = fail

    async def stream(self):
        self.consumed = True
        if self.fail:
            raise RuntimeError('connection interrupted')
        yield b'audio'


class AdmissionTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.finish = threading.Event()
        self.finished = threading.Event()
        self.calls = 0
        self.ns = dict(asyncio=asyncio, Path=Path, threading=threading,
                       os=os, json=json, time=time, secrets=secrets, shutil=shutil,
                       HTTPException=HTTPException, JSONResponse=Response, Request=Request,
                       DATA=Path(self.temp.name), ready=True, active=None,
                       lock=threading.RLock(), slot=threading.Lock(),
                       MAX_UPLOAD=1024, RESERVE=0, log=logging.getLogger('test'),
                       cleanup_old=lambda: None, struct=struct,
                   SPOOL_CIPHER=PassthroughCipher(), RECORD_BYTES=1024)
        self.ns['snapshot'] = lambda: {'active_job': self.ns['active']}

        def process(job):
            self.calls += 1
            self.finish.wait(5)
            self.ns['update'](job, persist=True, status='completed')
            self.ns['active'] = None
            self.ns['slot'].release()
            self.finished.set()

        self.ns['run_job'] = process
        tree = ast.parse(Path(__file__).with_name('gateway.py').read_text())
        functions = []
        for node in tree.body:
            if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)) and node.name in (
                    'atomic_json', 'job_dir', 'update', 'submit', 'seal_record'):
                node.decorator_list = []
                functions.append(node)
        exec(compile(ast.Module(body=functions, type_ignores=[]), 'gateway.py', 'exec'), self.ns)

    def tearDown(self):
        self.finish.set()
        if self.calls:
            self.finished.wait(5)
        self.temp.cleanup()

    def submit(self, request):
        return asyncio.run(self.ns['submit'](request))

    def test_same_identity_reattaches_while_running_and_after_completion(self):
        identity = 'a' * 32
        first = Request(identity)
        self.assertEqual(self.submit(first).status_code, 202)
        again = Request(identity)
        self.assertEqual(self.submit(again).content['job_id'], identity)
        self.assertFalse(again.consumed)
        self.assertEqual(self.submit(Request('b' * 32)).status_code, 429)
        self.finish.set()
        self.assertTrue(self.finished.wait(5))
        after = Request(identity)
        self.assertEqual(self.submit(after).status_code, 202)
        self.assertFalse(after.consumed)
        self.assertEqual(self.calls, 1)
        self.assertEqual(json.loads((Path(self.temp.name)/identity/'job.json').read_text())['status'], 'completed')

    def test_interrupted_upload_releases_slot_and_keeps_failed_identity(self):
        identity = 'c' * 32
        with self.assertRaises(RuntimeError):
            self.submit(Request(identity, fail=True))
        self.assertFalse(self.ns['slot'].locked())
        self.assertIsNone(self.ns['active'])
        self.assertEqual(self.submit(Request(identity)).status_code, 202)
        self.assertEqual(self.calls, 0)
        state = json.loads((Path(self.temp.name)/identity/'job.json').read_text())
        self.assertEqual(state['status'], 'failed')

    def test_invalid_identity_does_not_reserve_slot(self):
        with self.assertRaises(HTTPException):
            self.submit(Request('../invalid'))
        self.assertFalse(self.ns['slot'].locked())


if __name__ == '__main__':
    unittest.main()
