"""Prepare bounded 16 kHz windows. The hour test repeats speech, not a real meeting."""
import array
import json
import sys
import wave
from pathlib import Path

root = Path(__file__).resolve().parents[2]
source = root / 'benchmark/vocabulary/results/2026-10-16-followup'
out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
remote = '/home/ubuntu/qwen-q8-test/audio'

def read(path):
    with wave.open(str(path), 'rb') as f:
        assert (f.getframerate(), f.getnchannels(), f.getsampwidth()) == (16000, 1, 2)
        samples = array.array('h', f.readframes(f.getnframes()))
        if sys.byteorder != 'little':
            samples.byteswap()
        return samples

def write(id, pcm):
    samples = array.array('f', (v / 32768 for v in pcm))
    if sys.byteorder != 'little':
        samples.byteswap()
    (out / f'{id}.f32').write_bytes(samples.tobytes())
    return f'{id}\t{remote}/{id}.f32\n'

human = array.array('h')
human_sources = sorted(source.glob('libri-*.wav'))
for p in human_sources:
    human.extend(read(p))
assert len(human) > 30*16000
rows = {}
for seconds in (2.5, 5, 7.5, 10, 12.5, 15, 20, 30):
    lines = []
    for i in range(8):
        frames = int(seconds*16000)
        start = (i*17*16000) % (len(human) - frames)
        id = f'human-{seconds}s-{i}'
        lines.append(write(id, human[start:start+frames]))
    rows[seconds] = lines
    (out / f'{seconds}s.tsv').write_text(''.join(lines))
(out / 'mixed.tsv').write_text(''.join(rows[s][i] for i in (0,1) for s in (5,10,20,30)))
(out / 'mixed-safe.tsv').write_text(''.join([
    rows[2.5][0], rows[5][1], rows[5][2], rows[2.5][3], rows[2.5][4], rows[5][5]]))
(out / 'hour.tsv').write_text(''.join(rows[30][i%8] for i in range(120)))
for seconds in (5,10):
    (out / f'hour-{seconds}s.tsv').write_text(''.join(
        f'hour-{i:04d}-'+rows[seconds][i%8] for i in range(3600//seconds)))
references = []
for entry in json.loads((source / 'holdout-corpus.json').read_text()):
    pcm = read(source / entry['audio'])
    # Preserve whole examples for supplemental accuracy; exclude >30s.
    if len(pcm) <= 30*16000:
        row = write(entry['id'], pcm)
        references.append({**entry, 'manifest_row': row})
(out / 'accuracy.tsv').write_text(''.join(e['manifest_row'] for e in references))
(out / 'metadata.json').write_text(json.dumps({
    'sample_rate':16000, 'format':'little-endian float32 mono',
    'human_sources':[str(p.relative_to(root)) for p in human_sources],
    'source_seconds':len(human)/16000,
    'window_seconds':[2.5,5,7.5,10,12.5,15,20,30], 'mixed_seconds':[5,10,20,30,5,10,20,30],
    'hour_test':'120 sequential 30-second windows repeating eight speech fixtures; capacity/memory endurance only',
    'references':references,
}, indent=2))
print(json.dumps({'human_seconds':len(human)/16000,'accuracy_examples':len(references),'out':str(out)}))
