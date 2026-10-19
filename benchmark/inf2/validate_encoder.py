"""Compare explicit-state encoder against official dynamic streaming caches."""

import argparse
import json
import platform
import time
from pathlib import Path

import torch
from torch.nn import functional as F
from transformers import Nemotron3_5AsrConfig, Nemotron3_5AsrForRNNT
from transformers.cache_utils import DynamicCache
from transformers.models.nemotron_asr_streaming.modeling_nemotron_asr_streaming import (
    NemotronAsrStreamingEncoderCausalConvPaddingCache,
)

from streaming_encoder import StreamingEncoder, example_inputs, load_model


def reference_step(model, hidden, prompts, kv, conv):
    history = min(kv.get_seq_length(), 56)
    positions = model.encoder.encode_positions(hidden, cached_frames=history)
    mask = torch.ones(1, 1, 7, history + 7, dtype=torch.bool)
    for layer in model.encoder.layers:
        hidden = layer(hidden, attention_mask=mask, position_embeddings=positions,
                       past_key_values=kv, padding_cache=conv)
    prompts = prompts[:, None, :].expand(-1, 7, -1)
    return model.encoder_projector(model.prompt_projector(torch.cat((hidden, prompts), dim=-1)))


def tiny_model():
    config = Nemotron3_5AsrConfig(
        encoder_config={"model_type": "nemotron_asr_streaming_encoder", "hidden_size": 32,
                        "intermediate_size": 64, "num_hidden_layers": 2,
                        "num_attention_heads": 4, "num_key_value_heads": 4,
                        "conv_kernel_size": 9, "sliding_window": 57,
                        "subsampling_conv_channels": 4},
        decoder_hidden_size=32, prompt_intermediate_size=64,
    )
    config._attn_implementation = "eager"
    config.encoder_config._attn_implementation = "eager"
    return Nemotron3_5AsrForRNNT(config).eval()


@torch.inference_mode()
def validate(model, steps=14, check_jit=False):
    adapter = StreamingEncoder(model).eval()
    inputs = list(example_inputs(model, 2))
    # Different language conditioning on each lane.
    inputs[5] = F.one_hot(torch.tensor([0, 14]), model.config.num_prompts).float()
    states = [(DynamicCache(config=model.encoder.config),
               NemotronAsrStreamingEncoderCausalConvPaddingCache()) for _ in range(2)]
    if check_jit:
        adapter = torch.jit.trace(adapter, tuple(inputs), check_trace=False)
    max_error = 0.0
    state_error = 0.0
    calls = 0
    start = time.perf_counter()
    for step in range(steps):
        inputs[0] = torch.randn_like(inputs[0])
        inputs[6] = torch.tensor([True, step % 3 != 1])
        if step == 10:
            for i in [1, 2, 3]:
                inputs[i][:, 1].zero_()
            inputs[4][1] = 0
            states[1] = (DynamicCache(config=model.encoder.config),
                         NemotronAsrStreamingEncoderCausalConvPaddingCache())
        old_state = [x.clone() for x in inputs[1:5]]
        outputs = adapter(*inputs)
        for lane in range(2):
            if not inputs[6][lane]:
                assert torch.count_nonzero(outputs[0][lane]) == 0
                for i in range(3):
                    torch.testing.assert_close(outputs[i + 1][:, lane], old_state[i][:, lane], rtol=0, atol=0)
                torch.testing.assert_close(outputs[4][lane], old_state[3][lane], rtol=0, atol=0)
                continue
            kv, conv = states[lane]
            expected = reference_step(model, inputs[0][lane:lane+1], inputs[5][lane:lane+1], kv, conv)
            error = float((expected[0] - outputs[0][lane]).abs().max())
            max_error = max(max_error, error)
            torch.testing.assert_close(outputs[0][lane], expected[0], rtol=2e-4, atol=2e-4)
            for layer_idx, layer in enumerate(kv.layers):
                n = layer.keys.shape[2]
                for index, reference in [(1, layer.keys), (2, layer.values)]:
                    actual = outputs[index][layer_idx, lane:lane+1, :, -n:, :]
                    state_error = max(state_error, float((actual-reference).abs().max()))
                    # Independent B=1 oracle vs B=2 adapter uses different GEMM
                    # reduction orders. Deep trained layers accumulate small
                    # FP32 differences; retain the measured maximum in the report.
                    torch.testing.assert_close(actual, reference, rtol=1e-3, atol=1e-3,
                        msg=lambda message: f"step={step}, lane={lane}, layer={layer_idx}, state={index}: {message}")
                actual = outputs[3][layer_idx, lane:lane+1]
                reference = conv.layers[f"conv.{layer_idx}"].cache
                state_error = max(state_error, float((actual-reference).abs().max()))
                torch.testing.assert_close(actual, reference, rtol=1e-3, atol=1e-3,
                    msg=lambda message: f"step={step}, lane={lane}, layer={layer_idx}, conv: {message}")
            calls += 1
        inputs[1:5] = outputs[1:5]
    return {"passed": True, "layers": len(model.encoder.layers), "steps": steps,
            "active_stream_calls": calls, "max_output_abs_error": max_error,
            "max_state_abs_error": state_error, "seconds": time.perf_counter()-start,
            "jit": check_jit, "torch": torch.__version__, "host": platform.node(),
            "input_shapes": [list(x.shape) for x in inputs],
            "state_bytes_per_stream_fp32": sum(x.numel()*x.element_size()/2 for x in inputs[1:5]),
            "scope": "CPU numerical/state parity on subsampled frames; no Neuron performance claim"}


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--model", default=None)
    p.add_argument("--tiny", action="store_true")
    p.add_argument("--jit", action="store_true")
    p.add_argument("--threads", type=int, default=4)
    p.add_argument("--output", default="artifacts/cpu-parity.json")
    args = p.parse_args()
    torch.set_num_threads(args.threads)
    torch.manual_seed(42)
    model = tiny_model() if args.tiny else load_model(args.model) if args.model else load_model()
    result = validate(model, check_jit=args.jit)
    result["weights"] = "random small test model" if args.tiny else args.model or "official checkpoint"
    Path(args.output).parent.mkdir(parents=True, exist_ok=True)
    Path(args.output).write_text(json.dumps(result, indent=2)+"\n")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
