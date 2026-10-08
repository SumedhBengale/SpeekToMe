"""Non-journal log budget. Journal has its own separate storage limits."""
import os
from pathlib import Path

files = []
for base, directories, names in os.walk('/var/log', followlinks=False):
    directories[:] = [name for name in directories if name != 'journal']
    for name in names:
        path = Path(base)/name
        if path.is_file() and not path.is_symlink():
            info = path.stat()
            files.append((info.st_mtime, path, info.st_size))
total = sum(size for _, _, size in files)
for _, path, size in sorted(files):
    if total <= 64*1024**2:
        break
    with path.open('wb'):
        pass
    total -= size
