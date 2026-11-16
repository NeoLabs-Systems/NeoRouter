#!/usr/bin/env python3
"""Fetch this application's privacy-filter fixture into an isolated cache."""
import argparse
import json
import pathlib
import subprocess
import urllib.request

args = argparse.ArgumentParser()
args.add_argument('--cache', required=True)
args.add_argument('--dtype', default='q4')
options = args.parse_args()
root = pathlib.Path(__file__).resolve().parents[2]
model = subprocess.check_output(['node', '-e', "process.stdout.write(require('./server/services/processing/privacy_model').MODEL)"], cwd=root, text=True)
metadata = json.load(urllib.request.urlopen(f'https://huggingface.co/api/models/{model}', timeout=30))
revision = metadata['sha']
listing = json.load(urllib.request.urlopen(f'https://huggingface.co/api/models/{model}/tree/{revision}?recursive=false&expand=false', timeout=30))
files = [item['path'] for item in listing if item['type'] == 'file' and item['path'].endswith(('.json', '.model'))]
files += [f'onnx/model_{options.dtype}.onnx', f'onnx/model_{options.dtype}.onnx_data']
for name in files:
    target = pathlib.Path(options.cache) / model / name
    target.parent.mkdir(parents=True, exist_ok=True)
    if target.exists():
        continue
    with urllib.request.urlopen(f'https://huggingface.co/{model}/resolve/{revision}/{name}', timeout=60) as source, open(str(target) + '.part', 'wb') as out:
        while block := source.read(1024 * 1024):
            out.write(block)
    pathlib.Path(str(target) + '.part').rename(target)
    print(name, target.stat().st_size, 'bytes', flush=True)
print(json.dumps({'model': model, 'revision': revision, 'dtype': options.dtype}))
