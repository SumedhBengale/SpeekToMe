"""Feed real PCM in 560-ms increments; compare streaming outputs and tokens.

This is a correctness/capture tool, not a production server. End-of-stream
partial-chunk flush is deliberately outside its current scope and reported.
Captured projected frames can be replayed through the CPU decoder on Inf2.
"""

import argparse
import json
import time
from pathlib import Path

import numpy as np
import soundfile as sf
import torch
from torch.nn import functional as F
from transformers import AutoProcessor
from transformers.cache_utils import DynamicCache
from transformers.models.nemotron_asr_streaming.modeling_nemotron_asr_streaming import (
    NemotronAsrStreamingEncoderCausalConvPaddingCache,
)
from transformers.models.nemotron3_5_asr.generation_nemotron3_5_asr import Nemotron3_5AsrRNNTDecoderCache

from streaming_encoder import MODEL_ID, MODEL_REVISION, StreamingEncoder, example_inputs, load_model


class PCMFrontEnd:
    """Retain only analysis-window overlap; model history lives in caches."""

    def __init__(self, processor, language="en-US"):
        self.processor = processor
        self.language = language
        self.buffer = np.empty(0, dtype=np.float32)
        self.offset = 0
        self.next_start = 0
        self.first = True

    def accept(self, samples):
        samples = np.asarray(samples, dtype=np.float32)
        if samples.ndim != 1:
            raise ValueError("Expected mono PCM")
        self.buffer = np.concatenate((self.buffer, samples))
        chunks = []
        while True:
            needed = (self.processor.num_samples_first_audio_chunk if self.first
                      else self.processor.num_samples_per_audio_chunk)
            local_start = self.next_start - self.offset
            if local_start + needed > len(self.buffer):
                break
            first = self.first
            audio = self.buffer[local_start:local_start+needed]
            features = self.processor(
                audio, sampling_rate=16000, is_streaming=True,
                is_first_audio_chunk=first, language=self.language, return_tensors="pt",
            ).input_features
            frames = (self.processor.num_mel_frames_first_audio_chunk if first
                      else self.processor.num_mel_frames_per_audio_chunk)
            features = features[:, :frames, :]
            if features.shape[1] != frames:
                raise RuntimeError(f"Unexpected feature shape {features.shape}; expected {frames} frames")
            chunks.append(features)
            if first:
                self.next_start = frames*self.processor.feature_extractor.hop_length - self.processor.feature_extractor.n_fft//2
                self.first = False
            else:
                self.next_start += frames*self.processor.feature_extractor.hop_length
            discard = self.next_start-self.offset
            self.buffer = self.buffer[discard:]
            self.offset = self.next_start
        return chunks


class GreedyDecoder:
    def __init__(self, model):
        self.model = model
        self.cache = Nemotron3_5AsrRNNTDecoderCache(model.config)
        self.prediction = model.decoder(torch.tensor([[model.config.blank_token_id]]), cache=self.cache)
        self.tokens = []

    def accept(self, projected):
        new_tokens = []
        for frame in projected[0]:
            for _ in range(self.model.config.max_symbols_per_step):
                token = int(self.model.joint(self.prediction, frame[None, None, :]).argmax(-1).item())
                if token == self.model.config.blank_token_id:
                    break
                self.tokens.append(token)
                new_tokens.append(token)
                self.prediction = self.model.decoder(torch.tensor([[token]]), cache=self.cache)
        return new_tokens


@torch.inference_mode()
def main():
    p = argparse.ArgumentParser()
    p.add_argument("--model", default=None)
    p.add_argument("--audio", required=True)
    p.add_argument("--chunks", type=int, default=16)
    p.add_argument("--threads", type=int, default=4)
    p.add_argument("--language", default="en-US")
    p.add_argument("--output", default="artifacts/audio-parity.json")
    args = p.parse_args()
    torch.set_num_threads(args.threads)
    model = load_model(args.model) if args.model else load_model()
    processor = AutoProcessor.from_pretrained(args.model or MODEL_ID,
                                             **({} if args.model else {"revision": MODEL_REVISION}))
    processor.set_num_lookahead_tokens(6)
    audio, sr = sf.read(args.audio, dtype="float32")
    if sr != 16000 or audio.ndim != 1:
        raise SystemExit("Use a mono 16-kHz test WAV")
    front = PCMFrontEnd(processor, args.language)
    module = StreamingEncoder(model).eval()
    inputs = list(example_inputs(model, 1))
    prompt = processor._resolve_prompt_ids(args.language, 1)
    inputs[5] = F.one_hot(prompt, model.config.num_prompts).float()
    subsampling_cache = NemotronAsrStreamingEncoderCausalConvPaddingCache()
    reference_cache = DynamicCache(config=model.encoder.config)
    reference_padding = NemotronAsrStreamingEncoderCausalConvPaddingCache()
    decoder, reference_decoder = GreedyDecoder(model), GreedyDecoder(model)
    records, captured, feature_chunks = [], [], []
    peak_buffer = 0
    for n in range(args.chunks):
        pcm = audio[n*8960:(n+1)*8960]
        if len(pcm) != 8960:
            break
        features_list = front.accept(pcm)
        peak_buffer = max(peak_buffer, len(front.buffer))
        for features in features_list:
            feature_chunks.append(features.clone())
            started = time.perf_counter()
            inputs[0] = model.encoder.subsampling(features, padding_cache=subsampling_cache)
            if inputs[0].shape != (1, 7, 1024):
                raise RuntimeError(f"Unexpected subsampler output {inputs[0].shape}")
            result = module(*inputs)
            elapsed = time.perf_counter()-started
            # Transformers 5.18's eager full encoder builds an additive mask,
            # while its attention interprets the mask as bool, producing NaNs.
            # The official SDPA path builds a bool mask. Use that unmodified
            # official path as the waveform oracle; the adapter supplies its
            # own explicit bool mask to eager attention for compiler tracing.
            model.encoder.config._attn_implementation = "sdpa"
            ref = model.get_audio_features(input_features=features, prompt_ids=prompt,
                    use_cache=True, past_key_values=reference_cache, padding_cache=reference_padding,
                    num_lookahead_tokens=6).pooler_output
            model.encoder.config._attn_implementation = "eager"
            torch.testing.assert_close(result[0], ref, rtol=3e-4, atol=3e-4)
            start = time.perf_counter()
            tokens = decoder.accept(result[0])
            decode_seconds = time.perf_counter()-start
            ref_tokens = reference_decoder.accept(ref)
            assert tokens == ref_tokens, (tokens, ref_tokens)
            captured.append(result[0].clone())
            records.append({"chunk": n, "mel_frames": features.shape[1],
                            "encoder_max_abs_error": float((result[0]-ref).abs().max()),
                            "cpu_frontend_encoder_seconds": elapsed,
                            "cpu_decoder_seconds": decode_seconds, "tokens": tokens})
            print(json.dumps({"chunk": n, "text": processor.decode(tokens, skip_special_tokens=True)}), flush=True)
            inputs[1:5] = result[1:5]
    if not records or not decoder.tokens:
        raise RuntimeError("No complete chunks or no transcript; not a successful speech validation")
    model.encoder.config._attn_implementation = "sdpa"
    official = model.generate(input_features=(x for x in feature_chunks),
                              num_lookahead_tokens=6, prompt_ids=prompt,
                              return_dict_in_generate=True, max_new_tokens=2000)
    model.encoder.config._attn_implementation = "eager"
    official_tokens = [int(x) for x in official.sequences[0] if int(x) != model.config.blank_token_id]
    assert official_tokens == decoder.tokens, (official_tokens, decoder.tokens)
    report = {"passed": True, "scope": "CPU streaming-prefix parity; not real-time capacity or end-of-stream flush validation",
              "audio": str(Path(args.audio).resolve()), "input_samples_per_packet": 8960,
              "chunk_ms": 560, "encoder_calls": len(records), "records": records,
              "text": processor.decode(decoder.tokens, skip_special_tokens=True),
              "token_ids": decoder.tokens, "peak_retained_pcm_samples": peak_buffer,
              "encoder_state_fp32_bytes_per_stream": sum(x.numel()*x.element_size() for x in inputs[1:5]),
              "remaining_buffer_samples": len(front.buffer), "commit_validated": False,
              "official_generation_token_parity_passed": True}
    out = Path(args.output)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(report, indent=2)+"\n")
    torch.save({"projected_chunks": torch.cat(captured), "tokens": decoder.tokens,
                "source": report["audio"], "language": args.language}, out.with_suffix(".pt"))
    print(json.dumps({k:v for k,v in report.items() if k not in ["records", "token_ids"]}, indent=2))


if __name__ == "__main__":
    main()
