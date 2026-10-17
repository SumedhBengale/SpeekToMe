#!/usr/bin/env python3
"""Read memory for a SpeekToMe PID and its children; prints no process arguments."""
import argparse
import json
from pathlib import Path


def snapshot(root):
    processes = {}
    for directory in Path('/proc').iterdir():
        if not directory.name.isdigit():
            continue
        try:
            status = dict(line.split(':', 1) for line in (directory / 'status').read_text().splitlines() if ':' in line)
            processes[int(directory.name)] = (int(status['PPid']), status['Name'].strip())
        except (FileNotFoundError, PermissionError, ProcessLookupError):
            continue
    selected = {root}
    while True:
        children = {pid for pid, (parent, _) in processes.items() if parent in selected}
        if children <= selected:
            break
        selected |= children
    rows = []
    for pid in sorted(selected):
        if pid not in processes:
            continue
        row = {'pid': pid, 'name': processes[pid][1]}
        try:
            rollup = dict(line.split(':', 1) for line in Path(f'/proc/{pid}/smaps_rollup').read_text().splitlines() if ':' in line)
            row.update({key.lower() + '_kib': int(rollup[key].split()[0]) for key in ('Rss', 'Pss', 'Private_Clean', 'Private_Dirty')})
        except (FileNotFoundError, PermissionError, ProcessLookupError):
            row['memory_unavailable'] = True
        rows.append(row)
    return {'processes': rows, 'total_pss_kib': sum(row.get('pss_kib', 0) for row in rows), 'total_rss_kib': sum(row.get('rss_kib', 0) for row in rows)}


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('pid', type=int)
    args = parser.parse_args()
    print(json.dumps(snapshot(args.pid), indent=2))
