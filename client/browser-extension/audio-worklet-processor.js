class DictationProcessor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.buffer = new Float32Array(4096);
    this.offset = 0;
  }

  process(inputs, outputs) {
    const input = inputs[0]?.[0];
    if (input) {
      let sourceOffset = 0;
      while (sourceOffset < input.length) {
        const count = Math.min(input.length - sourceOffset, this.buffer.length - this.offset);
        this.buffer.set(input.subarray(sourceOffset, sourceOffset + count), this.offset);
        sourceOffset += count;
        this.offset += count;

        if (this.offset === this.buffer.length) {
          const chunk = this.buffer.buffer;
          this.port.postMessage(chunk, [chunk]);
          this.buffer = new Float32Array(4096);
          this.offset = 0;
        }
      }
    }

    // Keep the node alive without monitoring the microphone.
    const output = outputs[0];
    if (output) {
      for (const channel of output) channel.fill(0);
    }
    return true;
  }
}

registerProcessor('dictation-processor', DictationProcessor);
