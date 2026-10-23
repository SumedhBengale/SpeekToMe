"""Verify the CPU gateway never writes plaintext audio and decodes in memory."""
import ast
import contextlib
import json
import os
from pathlib import Path
import shutil
import struct
import subprocess
import tempfile
import threading
import unittest

import numpy as np
from cryptography.hazmat.primitives.ciphers.aead import AESGCM


def load(names):
    tree = ast.parse(Path(__file__).with_name('gateway.py').read_text())
    ns = dict(os=os, struct=struct, json=json, subprocess=subprocess, threading=threading,
              contextlib=contextlib, np=np, SPOOL_CIPHER=AESGCM(AESGCM.generate_key(bit_length=256)))
    body = [node for node in tree.body if isinstance(node, ast.FunctionDef) and node.name in names]
    exec(compile(ast.Module(body=body, type_ignores=[]), 'gateway.py', 'exec'), ns)
    return ns


class SealingTest(unittest.TestCase):
    def test_records_are_sealed_and_bound_to_job(self):
        ns = load({'seal_record', 'open_records'})
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)/'input.sealed'
            secret = b'OggS private meeting audio'
            path.write_bytes(ns['seal_record']('a'*32, 0, secret) + ns['seal_record']('a'*32, 1, secret))
            self.assertNotIn(b'private', path.read_bytes())
            self.assertEqual(list(ns['open_records'](path, 'a'*32)), [secret, secret])
            with self.assertRaises(Exception):
                list(ns['open_records'](path, 'b'*32))

    @unittest.skipUnless(shutil.which('ffmpeg') and shutil.which('ffprobe'), 'ffmpeg unavailable')
    def test_decode_from_sealed_upload_without_temporary_files(self):
        ns = load({'seal_record', 'open_records', 'decode_audio'})
        encode = subprocess.run(['ffmpeg', '-v', 'error', '-f', 'lavfi', '-i', 'sine=frequency=440:duration=2',
                                 '-ac', '1', '-ar', '16000', '-c:a', 'libopus', '-f', 'ogg', 'pipe:1'],
                                capture_output=True)
        if encode.returncode != 0:
            self.skipTest('ffmpeg without libopus')
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp)/'input.sealed'
            ogg = encode.stdout
            path.write_bytes(b''.join(ns['seal_record']('c'*32, i, ogg[i*4096:(i+1)*4096])
                                      for i in range((len(ogg)+4095)//4096)))
            audio = ns['decode_audio'](path, 'c'*32, 60, lambda seconds: None)
            self.assertAlmostEqual(len(audio)/16000, 2, delta=0.1)
            self.assertEqual(sorted(p.name for p in Path(temp).iterdir()), ['input.sealed'])
            with self.assertRaises(ValueError):
                ns['decode_audio'](path, 'c'*32, 0.5, lambda seconds: None)


if __name__ == '__main__':
    unittest.main()
