"""Application-layer encryption for diarization requests (format v1).

The coordinator seals audio to the diarizer's long-term X25519 public key; only the GPU
container holds the private key (a Modal Secret attached to that function alone). Modal's
input store, request path and logs therefore only ever see ciphertext. Mirrors
server/handlers/diarization_envelope.go byte for byte.

Request  = "SDE1" | key_id[8] | ephemeral_pub[32] | issued_at u64 | job_id[16]  (68-byte header)
           then records: u32 ciphertext length | AES-256-GCM(record), 1 MiB plaintext each.
Response = "SDR1" | job_id[16]  (20-byte header), then records sealed with the response key.

Keys: HKDF-SHA256(X25519(ephemeral, recipient), salt = ephemeral_pub | recipient_pub,
info = "speektome-diarization/v1/request" or ".../response"). Nonce = 4 zero bytes | u64 index.
AAD = header | u64 index | final flag, so records cannot be reordered, dropped, truncated or
moved between jobs. At least one record is always present; the last has final = 1.
"""
import hashlib
import struct

from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric.x25519 import X25519PrivateKey, X25519PublicKey
from cryptography.hazmat.primitives.ciphers.aead import AESGCM
from cryptography.hazmat.primitives.kdf.hkdf import HKDF

REQUEST_MAGIC = b'SDE1'
RESPONSE_MAGIC = b'SDR1'
REQUEST_HEADER = 68
RESPONSE_HEADER = 20
RECORD = 1 << 20
TAG = 16
MAX_RECORDS = 4096  # 4 GiB of audio; far above any admissible meeting


class EnvelopeError(ValueError):
    """Deliberately uninformative: callers report only that the envelope was rejected."""


def raw_public(key):
    return key.public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)


def key_id(public_raw: bytes) -> bytes:
    return hashlib.sha256(b'speektome-diarization-key/v1' + public_raw).digest()[:8]


def _keys(shared: bytes, ephemeral_pub: bytes, recipient_pub: bytes):
    def derive(label):
        return HKDF(hashes.SHA256(), 32, ephemeral_pub + recipient_pub,
                    b'speektome-diarization/v1/' + label).derive(shared)
    return derive(b'request'), derive(b'response')


def _nonce(index):
    return b'\0\0\0\0' + struct.pack('>Q', index)


def _seal_records(cipher, header, plaintext):
    count = max(1, -(-len(plaintext)//RECORD))
    out = bytearray(header)
    for index in range(count):
        chunk = plaintext[index*RECORD:(index+1)*RECORD]
        aad = header + struct.pack('>QB', index, index == count-1)
        sealed = cipher.encrypt(_nonce(index), chunk, aad)
        out += struct.pack('>I', len(sealed)) + sealed
    return bytes(out)


def _open_records(cipher, header, body):
    plain, offset, index, final = bytearray(), 0, 0, False
    while offset < len(body):
        if final or index >= MAX_RECORDS or offset+4 > len(body):
            raise EnvelopeError('rejected')
        (size,) = struct.unpack_from('>I', body, offset)
        offset += 4
        if size < TAG or size > RECORD+TAG or offset+size > len(body):
            raise EnvelopeError('rejected')
        final = offset+size == len(body)
        try:
            plain += cipher.decrypt(_nonce(index), body[offset:offset+size], header+struct.pack('>QB', index, final))
        except Exception as exc:
            raise EnvelopeError('rejected') from exc
        offset += size
        index += 1
    if not final:
        raise EnvelopeError('rejected')
    return bytes(plain)


def parse_request_header(envelope: bytes) -> dict:
    """Plaintext header fields; safe to check before any decryption (front endpoint)."""
    if len(envelope) < REQUEST_HEADER+4+TAG or envelope[:4] != REQUEST_MAGIC:
        raise EnvelopeError('rejected')
    (issued_at,) = struct.unpack_from('>Q', envelope, 44)
    return dict(key_id=envelope[4:12], ephemeral=envelope[12:44], issued_at=issued_at, job_id=envelope[52:68].hex())


def open_request(envelope: bytes, private_keys) -> tuple[bytes, str, bytes]:
    """Return (audio, job_id, response_key). `private_keys` maps key_id -> X25519PrivateKey."""
    header = parse_request_header(envelope)
    private = private_keys.get(header['key_id'])
    if private is None:
        raise EnvelopeError('rejected')
    recipient_pub = raw_public(private.public_key())
    try:
        shared = private.exchange(X25519PublicKey.from_public_bytes(header['ephemeral']))
    except Exception as exc:
        raise EnvelopeError('rejected') from exc
    request_key, response_key = _keys(shared, header['ephemeral'], recipient_pub)
    audio = _open_records(AESGCM(request_key), envelope[:REQUEST_HEADER], envelope[REQUEST_HEADER:])
    return audio, header['job_id'], response_key


def seal_response(plaintext: bytes, response_key: bytes, job_id: str) -> bytes:
    return _seal_records(AESGCM(response_key), RESPONSE_MAGIC+bytes.fromhex(job_id), plaintext)


def seal_request(plaintext: bytes, recipient_pub: bytes, job_id: str, issued_at: int, ephemeral=None):
    """Coordinator-side sealing (the Go client does this in production); used by tests and tools."""
    ephemeral = ephemeral or X25519PrivateKey.generate()
    ephemeral_pub = raw_public(ephemeral.public_key())
    shared = ephemeral.exchange(X25519PublicKey.from_public_bytes(recipient_pub))
    request_key, response_key = _keys(shared, ephemeral_pub, recipient_pub)
    header = REQUEST_MAGIC+key_id(recipient_pub)+ephemeral_pub+struct.pack('>Q', issued_at)+bytes.fromhex(job_id)
    return _seal_records(AESGCM(request_key), header, plaintext), response_key


def open_response(envelope: bytes, response_key: bytes, job_id: str) -> bytes:
    header = RESPONSE_MAGIC+bytes.fromhex(job_id)
    if envelope[:RESPONSE_HEADER] != header:
        raise EnvelopeError('rejected')
    return _open_records(AESGCM(response_key), header, envelope[RESPONSE_HEADER:])


def load_private_keys(*encoded: str) -> dict:
    """Base64 raw X25519 private keys (current first, then any still-accepted previous key)."""
    import base64
    keys = {}
    for value in encoded:
        if value:
            private = X25519PrivateKey.from_private_bytes(base64.b64decode(value, validate=True))
            keys[key_id(raw_public(private.public_key()))] = private
    if not keys:
        raise RuntimeError('diarization private key missing')
    return keys
