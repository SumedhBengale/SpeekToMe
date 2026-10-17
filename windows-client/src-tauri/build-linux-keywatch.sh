#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
source_file="$script_dir/linux-keywatch.c"
output_file="$script_dir/../target/release/speektome-keywatch"

if [ ! -f "$source_file" ]; then
  echo "Linux shortcut helper source not found: $source_file" >&2
  exit 1
fi

mkdir -p "$(dirname -- "$output_file")"
cc -O2 -Wall -Wextra -std=c11 "$source_file" -o "$output_file"
chmod 0755 "$output_file"
