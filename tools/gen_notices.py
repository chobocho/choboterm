"""Writes THIRD_PARTY_NOTICES.txt: the licenses of everything built into choboterm.exe.

Run from the repository root after changing Go or npm dependencies:
    python tools/gen_notices.py
"""
import json
import os
import subprocess

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LICENSE_NAMES = ('LICENSE', 'LICENSE.txt', 'LICENSE.md', 'LICENCE', 'COPYING')


def read_license(d):
    for name in LICENSE_NAMES:
        p = os.path.join(d, name)
        if os.path.isfile(p):
            with open(p, encoding='utf-8', errors='replace') as f:
                return f.read().strip()
    raise SystemExit(f'no license file in {d}')


def go(*args):
    return subprocess.run(['go', *args], cwd=ROOT, check=True, capture_output=True, text=True).stdout


def go_modules():
    """Go modules linked into the Wails production build."""
    out = go('list', '-tags', 'desktop,production', '-deps', '-f',
             '{{if .Module}}{{.Module.Path}} {{.Module.Version}}{{end}}', '.')
    mods = sorted({tuple(l.split()) for l in out.splitlines() if len(l.split()) == 2})
    cache = go('env', 'GOMODCACHE').strip()
    for path, ver in mods:
        # The module cache escapes upper-case letters as !lower.
        esc = ''.join('!' + c.lower() if c.isupper() else c for c in path)
        yield f'{path} {ver}', read_license(os.path.join(cache, f'{esc}@{ver}'))


def npm_packages():
    """npm packages bundled into the frontend (production dependencies)."""
    out = subprocess.run('npm ls --omit=dev --all --parseable', cwd=os.path.join(ROOT, 'frontend'),
                         shell=True, capture_output=True, text=True).stdout
    dirs = sorted({l.strip() for l in out.splitlines() if 'node_modules' in l})
    for d in dirs:
        with open(os.path.join(d, 'package.json'), encoding='utf-8') as f:
            pkg = json.load(f)
        yield f'{pkg["name"]} {pkg["version"]} (npm)', read_license(d)


def main():
    goroot = go('env', 'GOROOT').strip()
    entries = [(f'Go standard library {go("env", "GOVERSION").strip()}', read_license(goroot))]
    entries += list(go_modules()) + list(npm_packages())
    sep = '-' * 78
    parts = ['choboterm includes the following third-party software.\n'
             'Each is distributed under its own license, reproduced below.\n']
    parts += [f'{sep}\n{name}\n{sep}\n\n{text}\n' for name, text in entries]
    with open(os.path.join(ROOT, 'THIRD_PARTY_NOTICES.txt'), 'w', encoding='utf-8', newline='\n') as f:
        f.write('\n'.join(parts))
    print(f'{len(entries)} entries')


if __name__ == '__main__':
    main()
