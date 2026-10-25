"""python -m unittest test_envelope  (vectors are shared with server/handlers/diarization_envelope_test.go)"""
import base64
import hashlib
import struct
import unittest

from cryptography.hazmat.primitives.asymmetric.x25519 import X25519PrivateKey

import envelope as e

JOB = '00112233445566778899aabbccddeeff'
ISSUED = 1793000000
RECIPIENT = X25519PrivateKey.from_private_bytes(bytes(range(32)))
EPHEMERAL = X25519PrivateKey.from_private_bytes(bytes(range(32, 64)))
PLAIN = bytes((i*7+3) % 251 for i in range(e.RECORD+100))
REQUEST_SHA256 = '411033b3b93e2649acadd112a3275705fd86ca695f1e69b99a5410ee68141cb3'
RESPONSE_KEY = '65cbd8a322004140c4ac54b4dde2f46173c377bc1a22a7698f99f04819791963'
RESPONSE = bytes.fromhex(
    '5344523100112233445566778899aabbccddeeff000000431d98fc4bbc860af0b197a21efcc58edc996b9a5c872691c825c22d4f'
    '72f46d647069344027b9b1c42aafb4d621d071820f7323907b6b3b8b450ccb7fb0e44d1ffc733b')
RESULT = b'{"segments":[{"start":0.5,"end":1.25,"speaker":1}]}'


def keys():
    return e.load_private_keys(base64.b64encode(RECIPIENT.private_bytes_raw()).decode())


def sealed(plain=PLAIN, job=JOB):
    return e.seal_request(plain, e.raw_public(RECIPIENT.public_key()), job, ISSUED, EPHEMERAL)


class Envelope(unittest.TestCase):
    def test_vector_matches_go(self):
        request, response_key = sealed()
        self.assertEqual(hashlib.sha256(request).hexdigest(), REQUEST_SHA256)
        self.assertEqual(response_key.hex(), RESPONSE_KEY)
        self.assertEqual(e.seal_response(RESULT, response_key, JOB), RESPONSE)

    def test_round_trip_and_header(self):
        request, response_key = sealed()
        header = e.parse_request_header(request)
        self.assertEqual((header['job_id'], header['issued_at']), (JOB, ISSUED))
        audio, job, key = e.open_request(request, keys())
        self.assertEqual((audio, job, key), (PLAIN, JOB, response_key))
        self.assertEqual(e.open_response(RESPONSE, response_key, JOB), RESULT)

    def test_empty_audio_is_one_final_record(self):
        request, _ = sealed(b'')
        self.assertEqual(len(request), e.REQUEST_HEADER+4+e.TAG)
        self.assertEqual(e.open_request(request, keys())[0], b'')

    def test_rejects_tampering(self):
        request, response_key = sealed()
        first = struct.unpack_from('>I', request, e.REQUEST_HEADER)[0]
        split = e.REQUEST_HEADER+4+first
        flipped = bytearray(request)
        flipped[split-1] ^= 1
        header_changed = bytearray(request)
        header_changed[50] ^= 1  # issued_at is authenticated through the AAD
        cases = {
            'flipped bit': bytes(flipped),
            'header edited': bytes(header_changed),
            'truncated to first record': request[:split],
            'final record dropped and padded': request[:split]+b'\0'*4,
            'trailing bytes': request+b'x',
            'records swapped': request[:e.REQUEST_HEADER]+request[split:]+request[e.REQUEST_HEADER:split],
            'other job': request[:52]+bytes(16)+request[68:],
        }
        for name, bad in cases.items():
            with self.subTest(name), self.assertRaises(e.EnvelopeError):
                e.open_request(bad, keys())
        with self.assertRaises(e.EnvelopeError):
            e.open_request(request, e.load_private_keys(base64.b64encode(EPHEMERAL.private_bytes_raw()).decode()))
        with self.assertRaises(e.EnvelopeError):
            e.open_response(RESPONSE, response_key, 'ff'*16)
        with self.assertRaises(e.EnvelopeError):
            e.open_response(RESPONSE[:-1], response_key, JOB)

    def test_previous_key_still_accepted(self):
        other = X25519PrivateKey.generate()
        both = e.load_private_keys(base64.b64encode(other.private_bytes_raw()).decode(),
                                   base64.b64encode(RECIPIENT.private_bytes_raw()).decode())
        self.assertEqual(e.open_request(sealed()[0], both)[0], PLAIN)


if __name__ == '__main__':
    unittest.main()
