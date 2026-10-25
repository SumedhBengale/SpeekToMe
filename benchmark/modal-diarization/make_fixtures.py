"""Build mono 24 kb/s Ogg/Opus fixtures (the desktop client's bitrate) from AMI Mix-Headset WAVs.

    python make_fixtures.py /path/to/ami fixtures/

m017.opus is ES2004a alone (17.5 min, the model card's parity file); h1/h4 concatenate distinct
test-split meetings up to 1 and 4 hours, so long files have natural speech rather than repeats.
AMI audio is CC BY 4.0 (https://groups.inf.ed.ac.uk/ami/corpus/license.shtml).
"""
import subprocess
import sys
from pathlib import Path

ORDER = ['ES2004a', 'ES2004b', 'ES2004c', 'ES2004d', 'IS1009a', 'IS1009b', 'IS1009c', 'IS1009d',
         'TS3003a', 'TS3003b', 'TS3003c', 'TS3003d', 'EN2002a', 'EN2002b', 'EN2002c', 'EN2002d']


def duration(path):
    return float(subprocess.run(['ffprobe', '-v', 'error', '-show_entries', 'format=duration', '-of', 'csv=p=0', path],
                                check=True, capture_output=True, text=True).stdout)


def encode(sources, seconds, out):
    listing = out.with_suffix('.txt')
    listing.write_text(''.join(f"file '{s.resolve()}'\n" for s in sources))
    subprocess.run(['ffmpeg', '-y', '-v', 'error', '-f', 'concat', '-safe', '0', '-i', listing, '-t', str(seconds),
                    '-ac', '1', '-ar', '16000', '-c:a', 'libopus', '-b:a', '24k', '-application', 'voip', out], check=True)
    listing.unlink()
    print(f'{out.name}: {duration(out)/60:.1f} min, {out.stat().st_size/1e6:.1f} MB')


def main(ami, out):
    ami, out = Path(ami), Path(out)
    out.mkdir(parents=True, exist_ok=True)
    wavs = [ami/f'{m}.wav' for m in ORDER]
    encode(wavs[:1], duration(wavs[0]), out/'m017.opus')
    for name, target in (('h1.opus', 3600), ('h4.opus', 4*3600)):
        picked, total = [], 0.0
        while total < target:
            for wav in wavs:
                picked.append(wav)
                total += duration(wav)
                if total >= target:
                    break
        encode(picked, target, out/name)


if __name__ == '__main__':
    main(*sys.argv[1:3])
