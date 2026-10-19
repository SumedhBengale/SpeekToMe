"""Fixed-shape, explicit-state Nemotron 3.5 encoder for Neuron tracing.

The official streaming subsampler runs on CPU in this first prototype. Each
accelerator invocation consumes seven NEW subsampled frames (560 ms), never
the audio history. Model weights and layer implementations come from the
official Hugging Face checkpoint; quantized C weights are not reused.
"""

import torch
from torch import nn
from torch.nn import functional as F

MODEL_ID = "nvidia/nemotron-3.5-asr-streaming-0.6b"
MODEL_REVISION = "ea30d66debe3740a08b573244286791d423d6b3e"
CHUNK_FRAMES = 7
LEFT_CONTEXT = 56
CONV_CONTEXT = 8


def load_model(path=MODEL_ID):
    from transformers import Nemotron3_5AsrForRNNT

    kwargs = {} if path != MODEL_ID else {"revision": MODEL_REVISION}
    return Nemotron3_5AsrForRNNT.from_pretrained(
        path, attn_implementation="eager", dtype=torch.float32, **kwargs
    ).eval()


class ExplicitKVCache:
    """A transient Python wrapper; only its tensor operations enter the trace."""

    def __init__(self, keys, values):
        self.keys = keys.unbind(0)
        self.values = values.unbind(0)
        self.next_keys = []
        self.next_values = []

    def update(self, key, value, layer_idx):
        key = torch.cat((self.keys[layer_idx], key), dim=2)
        value = torch.cat((self.values[layer_idx], value), dim=2)
        self.next_keys.append(key[:, :, -LEFT_CONTEXT:, :])
        self.next_values.append(value[:, :, -LEFT_CONTEXT:, :])
        return key, value


class ExplicitConvCache:
    def __init__(self, convolution):
        self.convolution = convolution.unbind(0)
        self.next_convolution = []

    def update(self, hidden_states, cache_key, conv_module):
        layer = int(cache_key.split(".")[-1])
        joined = torch.cat((self.convolution[layer], hidden_states), dim=2)
        self.next_convolution.append(joined[:, :, -CONV_CONTEXT:])
        return joined


class StreamingEncoder(nn.Module):
    """All 24 FastConformer blocks, prompt fusion and encoder projection.

    Inputs:
      hidden: [B, 7, 1024], new subsampled frames
      keys, values: [24, B, 8, 56, 128], right-aligned valid history
      convolution: [24, B, 1024, 8], causal GLU history
      lengths: [B], int32 count of valid cached attention frames (0..56)
      prompts: [B, 128], float one-hot locale ID
      active: [B], bool; padding lanes leave all state unchanged

    Outputs: projected encoder frames [B,7,640], updated K/V, convolution,
    and lengths. Reset a stream by zeroing its state and lengths before use.
    Never schedule two outstanding invocations for the same stream slot.
    """

    def __init__(self, model):
        super().__init__()
        self.layers = model.encoder.layers
        self.prompt_projector = model.prompt_projector
        self.encoder_projector = model.encoder_projector
        self.register_buffer(
            "positions",
            model.encoder.encode_positions(
                torch.zeros(1, CHUNK_FRAMES, model.encoder.config.hidden_size),
                cached_frames=LEFT_CONTEXT,
            ),
        )
        self.register_buffer("key_indices", torch.arange(LEFT_CONTEXT + CHUNK_FRAMES))

    def forward(self, hidden, keys, values, convolution, lengths, prompts, active):
        kv = ExplicitKVCache(keys, values)
        conv = ExplicitConvCache(convolution)
        # Chunked attention sees the entire seven-frame chunk and valid history.
        valid = self.key_indices[None, :] >= LEFT_CONTEXT - lengths[:, None]
        attention_mask = valid[:, None, None, :].expand(-1, 1, CHUNK_FRAMES, -1)
        positions = self.positions.expand(hidden.shape[0], -1, -1)
        for layer in self.layers:
            hidden = layer(
                hidden,
                attention_mask=attention_mask,
                position_embeddings=positions,
                past_key_values=kv,
                padding_cache=conv,
            )
        prompts = prompts[:, None, :].expand(-1, CHUNK_FRAMES, -1)
        fused = self.prompt_projector(torch.cat((hidden, prompts), dim=-1))
        output = self.encoder_projector(fused)
        kv_active = active[None, :, None, None, None]
        conv_active = active[None, :, None, None]
        return (
            torch.where(active[:, None, None], output, torch.zeros_like(output)),
            torch.where(kv_active, torch.stack(kv.next_keys), keys),
            torch.where(kv_active, torch.stack(kv.next_values), values),
            torch.where(conv_active, torch.stack(conv.next_convolution), convolution),
            torch.where(active, (lengths + CHUNK_FRAMES).clamp(max=LEFT_CONTEXT), lengths),
        )


def example_inputs(model, batch, dtype=torch.float32):
    c = model.encoder.config
    return (
        torch.randn(batch, CHUNK_FRAMES, c.hidden_size, dtype=dtype),
        torch.zeros(len(model.encoder.layers), batch, c.num_attention_heads, LEFT_CONTEXT,
                    c.hidden_size // c.num_attention_heads, dtype=dtype),
        torch.zeros(len(model.encoder.layers), batch, c.num_attention_heads, LEFT_CONTEXT,
                    c.hidden_size // c.num_attention_heads, dtype=dtype),
        torch.zeros(len(model.encoder.layers), batch, c.hidden_size, CONV_CONTEXT, dtype=dtype),
        torch.zeros(batch, dtype=torch.int32),
        F.one_hot(torch.zeros(batch, dtype=torch.long), model.config.num_prompts).to(dtype),
        torch.ones(batch, dtype=torch.bool),
    )
