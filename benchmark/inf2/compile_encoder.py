"""Compile a fixed batch with no CPU partitioning, then measure encoder-only I/O.

This first diagnostic graph copies explicit state at the PyTorch boundary. Its
rate must not be advertised as production capacity or a device-resident result.
"""

import argparse
import json
import platform
import time
from pathlib import Path

import numpy as np
import torch

from streaming_encoder import MODEL_REVISION, StreamingEncoder, example_inputs, load_model


@torch.inference_mode()
def main():
    p = argparse.ArgumentParser()
    p.add_argument("--model", default=None)
    p.add_argument("--batch", type=int, required=True)
    p.add_argument("--cpu-compile", action="store_true")
    p.add_argument("--warmup", type=int, default=10)
    p.add_argument("--iterations", type=int, default=100)
    p.add_argument("--output-dir", default="artifacts/neuron")
    args = p.parse_args()
    if args.batch < 1 or args.iterations < 1:
        p.error("batch and iterations must be positive")
    import torch_neuronx
    torch.set_num_threads(2)
    model = load_model(args.model) if args.model else load_model()
    module = StreamingEncoder(model).eval().to(torch.bfloat16)
    inputs = example_inputs(model, args.batch, dtype=torch.bfloat16)
    out = Path(args.output_dir) / f"b{args.batch}"
    out.mkdir(parents=True, exist_ok=True)
    metadata = {"batch": args.batch, "revision": MODEL_REVISION, "torch": torch.__version__,
                "host": platform.node(), "state_resident": False, "cpu_partitioning": False,
                "input_shapes": [list(x.shape) for x in inputs],
                "input_dtypes": [str(x.dtype) for x in inputs],
                "scope": "encoder-only diagnostic, CPU subsampler and RNN-T excluded"}
    start = time.perf_counter()
    # No PartitionerConfig: unsupported operators fail instead of silently
    # splitting the 24-layer graph between the host and accelerator.
    compiled = torch_neuronx.trace(
        module, inputs, compiler_workdir=str(out / "compiler"),
        compiler_args=["--target", "inf2"], cpu_backend=args.cpu_compile,
    )
    metadata["compile_seconds"] = time.perf_counter()-start
    torch.jit.save(compiled, str(out / "encoder.pt"))
    metadata["graph"] = str(compiled.inlined_graph)
    (out / "compile.json").write_text(json.dumps(metadata, indent=2))
    if args.cpu_compile:
        print("Compiled for Inf2; inference not executed.")
        return
    reference = module(*inputs)
    actual = compiled(*inputs)
    differences = [float((a.float()-b.float()).abs().max()) for a,b in zip(actual, reference)]
    metadata["initial_output_max_abs_differences"] = differences
    # A first numerical screen only. Long streaming transcript/accuracy tests
    # remain mandatory before accepting a compiled model.
    for actual_tensor, reference_tensor in zip(actual, reference):
        torch.testing.assert_close(actual_tensor.float(), reference_tensor.float(), rtol=0.05, atol=0.05)
    metadata["initial_numeric_screen_passed"] = True
    inputs = list(inputs)
    timings = []
    for i in range(args.warmup + args.iterations):
        start = time.perf_counter()
        outputs = compiled(*inputs)
        # Outputs are CPU tensors: the call measures state I/O as well as compute.
        inputs[1:5] = outputs[1:5]
        elapsed = time.perf_counter()-start
        if i >= args.warmup:
            timings.append(elapsed)
    cps = args.batch / float(np.mean(timings))
    result = {**metadata, "chunks_per_second": cps,
              "theoretical_streams": cps*0.560,
              "batch_p50_ms": float(np.percentile(timings,50))*1000,
              "batch_p95_ms": float(np.percentile(timings,95))*1000,
              "batch_p99_ms": float(np.percentile(timings,99))*1000,
              "seconds": timings, "production_capacity_validated": False}
    (out / "benchmark.json").write_text(json.dumps(result, indent=2))
    print(json.dumps({k: v for k,v in result.items() if k not in ["graph", "seconds"]}, indent=2))


if __name__ == "__main__":
    main()
