"""Exercise the production routes against the installed FastAPI/Starlette stack.

No model loading, credentials, cloud services or inference calls are used.
"""
import importlib.util
import json
import os
from pathlib import Path
import sys
import tempfile
import types
import unittest
from unittest.mock import patch

from fastapi.testclient import TestClient


class GatewayFrameworkTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory()
        root = Path(cls.tmp.name)
        token = root / 'token'
        token.write_text('framework-compatibility-test-token')
        fake_model = types.ModuleType('diarize')
        fake_model.OnnxDiarizer = object
        fake_model.extract_speaker_dict = lambda *args: {}
        fake_model.segments_to_rttm = lambda *args: ''
        env = {'DIAR_ROOT': str(root), 'DIAR_DATA': str(root / 'jobs'),
               'DIAR_TOKEN_FILE': str(token)}
        spec = importlib.util.spec_from_file_location('gateway_framework_fixture',
                                                     Path(__file__).with_name('gateway.py'))
        cls.gateway = importlib.util.module_from_spec(spec)
        with patch.dict(os.environ, env), patch.dict(sys.modules, {'diarize': fake_model}):
            spec.loader.exec_module(cls.gateway)
        cls.client = TestClient(cls.gateway.app)
        cls.headers = {'Authorization': 'Bearer ' + token.read_text()}
        cls.job_id = 'a' * 32
        job = cls.gateway.DATA / cls.job_id
        job.mkdir()
        (job / 'job.json').write_text(json.dumps({'job_id': cls.job_id,
            'status': 'completed', 'created_at': 1, 'finished_at': 2}))
        (job / 'result.json').write_text('{"segments":[]}')

    @classmethod
    def tearDownClass(cls):
        cls.client.close()
        cls.tmp.cleanup()

    def test_authentication_still_precedes_routes(self):
        response = self.client.get('/v1/diarizations/' + self.job_id + '/result')
        self.assertEqual(response.status_code, 401)
        self.assertEqual(response.headers['www-authenticate'], 'Bearer')

    def test_completed_result_download(self):
        response = self.client.get('/v1/diarizations/' + self.job_id + '/result',
                                   headers=self.headers)
        self.assertEqual(response.status_code, 200)
        self.assertEqual(response.json(), {'segments': []})

    def test_normal_range_download(self):
        response = self.client.get('/v1/diarizations/' + self.job_id + '/result',
                                   headers={**self.headers, 'Range': 'bytes=0-3'})
        self.assertEqual(response.status_code, 206)
        self.assertEqual(response.content, b'{"se')


if __name__ == '__main__':
    unittest.main()
