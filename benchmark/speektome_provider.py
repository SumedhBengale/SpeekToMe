import io
import os
import time
import subprocess
import tempfile
import requests
from typing import Optional

from . import APIProvider, register, PermanentError


@register("speektome")
class SpeekToMeProvider(APIProvider):
    def __init__(self):
        self.server_url = os.environ.get("SPEEKTOME_SERVER_URL", "http://127.0.0.1:8000").rstrip("/")
        self.session_token = os.environ.get("SPEEKTOME_SESSION_TOKEN", "")

    def _convert_to_opus(self, input_path: str) -> bytes:
        cmd = [
            "ffmpeg",
            "-y",
            "-i", input_path,
            "-ac", "1",
            "-ar", "48000",
            "-c:a", "libopus",
            "-b:a", "32k",
            "-f", "ogg",
            "pipe:1",
        ]
        proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        out, err = proc.communicate()
        if proc.returncode != 0:
            raise RuntimeError(f"FFmpeg error converting to Opus: {err.decode('utf-8', errors='ignore')}")
        return out

    def transcribe(
        self,
        model_variant: str,
        audio_file_path: Optional[str],
        sample: dict,
        use_url: bool = False,
        language: str = "en",
        prompt: Optional[str] = None,
    ) -> str:
        token = self.session_token or os.environ.get("SPEEKTOME_SESSION_TOKEN", "")
        if not token:
            raise PermanentError("SPEEKTOME_SESSION_TOKEN environment variable is required.")

        enhance_mode = "true"
        if "raw" in model_variant.lower() or "whisper" in model_variant.lower() or "no-enhance" in model_variant.lower():
            enhance_mode = "false"

        temp_audio = None
        try:
            if use_url:
                url = sample["row"]["audio"][0]["src"]
                resp = requests.get(url, timeout=30)
                resp.raise_for_status()
                with tempfile.NamedTemporaryFile(suffix=".wav", delete=False) as f:
                    f.write(resp.content)
                    temp_audio = f.name
                input_file = temp_audio
            else:
                input_file = audio_file_path

            opus_data = self._convert_to_opus(input_file)

            headers = {
                "Authorization": f"Bearer {token}",
            }
            files = {
                "file": ("audio.ogg", io.BytesIO(opus_data), "audio/ogg"),
            }
            data = {
                "enhance": enhance_mode,
            }
            if prompt:
                data["prompt"] = prompt

            endpoint = f"{self.server_url}/generate"
            res = requests.post(endpoint, headers=headers, files=files, data=data, timeout=120)

            if res.status_code == 401 or res.status_code == 403:
                raise PermanentError(f"SpeekTo.Me authentication/quota error ({res.status_code}): {res.text}")

            if res.status_code != 200:
                raise Exception(f"SpeekTo.Me request failed with status {res.status_code}: {res.text}")

            res_json = res.json()
            return (res_json.get("text") or "").strip()

        finally:
            if temp_audio and os.path.exists(temp_audio):
                try:
                    os.remove(temp_audio)
                except Exception:
                    pass
