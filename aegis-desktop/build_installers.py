#!/usr/bin/env python3
"""Build the AEGIS Suite installers (Linux + Windows).

Generates per-OS payload zips (companion binary + icons + the
seven device-program packages + manifest with SHA-256 of every
file), compiles the stdlib-only Go installer for both OSes into
DOWNLOADS/SUITE-Aegis-Family/, and bundles them in one suite zip.
TALON is excluded by construction (never referenced here).
"""
import hashlib, json, os, shutil, subprocess, sys, zipfile
from pathlib import Path

HOME = Path('/home/user')
DESK = HOME / 'aegis-desktop'
SRC = DESK / 'installer'
BUILD = Path('/home/user/.suite-build')
OUTDIR = HOME / 'DOWNLOADS/SUITE-Aegis-Family'
COMP_ZIP = HOME / 'DOWNLOADS/COMPANION-Aegis-Desktop/Aegis-Companion-Desktop-v1.8.7.zip'

PROGRAMS = [  # (folder, zip path, blurb) - TALON absent on purpose
    ('PHOENIX', HOME/'DOWNLOADS/BLUE-Aegis-Phoenix/Aegis-Phoenix-v0.12.10-SD.zip'),
    ('SCRIBE',  HOME/'DOWNLOADS/SCRIBE-Aegis-Scribe/Aegis-Scribe-v0.1.7-SD.zip'),
    ('HEARTH',  HOME/'DOWNLOADS/HEARTH-Aegis-Hearth/Aegis-Hearth-v0.1.7-SD.zip'),
    ('HERALD',  HOME/'DOWNLOADS/HERALD-Aegis-Herald/Aegis-Herald-v0.1.6-CYD.zip'),
    ('KEEPER',  HOME/'DOWNLOADS/KEEPER-Aegis-Keeper/Aegis-Keeper-v0.1.3-SD.zip'),
    ('BENCH',   HOME/'DOWNLOADS/BENCH-Aegis-Bench/Aegis-Bench-v0.1.3-SD.zip'),
    ('LANTERN', HOME/'DOWNLOADS/LANTERN-Aegis-Lantern/Aegis-Lantern-v0.1.3-SD.zip'),
]

def sha(p): return hashlib.sha256(Path(p).read_bytes()).hexdigest()

def make_payload(osname, comp_member, comp_arc):
    stage = BUILD / f'payload-{osname}'
    shutil.rmtree(stage, ignore_errors=True)
    stage.mkdir(parents=True)
    with zipfile.ZipFile(COMP_ZIP) as z:
        (stage / comp_arc).write_bytes(z.read(comp_member))
        (stage / 'icon.ico').write_bytes(z.read('icon.ico'))
        (stage / 'icon.png').write_bytes(z.read('icon.png'))
    files = [{'path': comp_arc}, {'path': 'icon.ico'}, {'path': 'icon.png'}]
    for prog, zpath in PROGRAMS:
        arc = f'Suite/{prog}/{zpath.name}'
        dst = stage / arc
        dst.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy(zpath, dst)
        files.append({'path': arc})
    shutil.copy(DESK / 'SUITE-README.txt', stage / 'Suite/SUITE-README.txt')
    files.append({'path': 'Suite/SUITE-README.txt'})
    for f in files:
        p = stage / f['path']
        f['sha256'] = sha(p)
        f['size'] = p.stat().st_size
    manifest = {'suite': '1.2.17', 'companion': '1.8.7',
                'programs': [p for p, _ in PROGRAMS], 'files': files}
    (stage / 'suite-manifest.json').write_text(json.dumps(manifest, indent=2))
    out = BUILD / f'payload-{osname}.zip'
    with zipfile.ZipFile(out, 'w', zipfile.ZIP_DEFLATED) as z:
        for p in sorted(stage.rglob('*')):
            if p.is_file():
                z.write(p, p.relative_to(stage).as_posix())
    print(f'payload-{osname}.zip', f'{out.stat().st_size/1e6:.1f} MB,', len(files), 'files')
    return out

def main():
    go = '/home/user/.goroot/bin/go'
    if not Path(go).exists():
        sys.exit('Go SDK missing at /tmp/gosdk (restore it first)')
    shutil.rmtree(BUILD, ignore_errors=True)
    (BUILD / 'src').mkdir(parents=True)
    for f in SRC.glob('*.go'):
        shutil.copy(f, BUILD / 'src')
    (BUILD / 'src/go.mod').write_text('module aegissuite\n\ngo 1.24\n')
    make_payload('linux', 'AegisCompanion', 'AegisCompanion')
    make_payload('windows', 'AegisCompanion.exe', 'AegisCompanion.exe')
    shutil.copy(BUILD / 'payload-linux.zip', BUILD / 'src/payload-linux.zip')
    shutil.copy(BUILD / 'payload-windows.zip', BUILD / 'src/payload-windows.zip')
    OUTDIR.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, CGO_ENABLED='0', GOFLAGS='-mod=mod', GOTOOLCHAIN='go1.24.0', GOCACHE='/home/user/.buildtmp/gocache',
               GOPATH='/home/user/.buildtmp/gopath', GOTMPDIR='/home/user/.buildtmp/gotmp')
    Path('/home/user/.buildtmp/gotmp').mkdir(parents=True, exist_ok=True)
    targets = [('linux', 'Aegis-Suite-Setup-Linux'), ('windows', 'Aegis-Suite-Setup-Windows.exe')]
    for goos, name in targets:
        e = dict(env, GOOS=goos, GOARCH='amd64')
        subprocess.run([go, 'build', '-ldflags', '-s -w', '-o', str(OUTDIR / name), '.'],
                       cwd=BUILD / 'src', env=e, check=True)
        print('built', name, f'{(OUTDIR/name).stat().st_size/1e6:.1f} MB', sha(OUTDIR / name))
    # one-download bundle of both installers
    bundle = HOME / 'DOWNLOADS/SUITE-Aegis-Family/Aegis-Family-Suite-v1.2.17.zip'
    with zipfile.ZipFile(bundle, 'w', zipfile.ZIP_DEFLATED) as z:
        for _, name in targets:
            zi = zipfile.ZipInfo(name)
            zi.external_attr = 0o755 << 16
            zi.compress_type = zipfile.ZIP_DEFLATED
            z.writestr(zi, (OUTDIR / name).read_bytes())
        z.write(DESK / 'SUITE-README.txt', 'SUITE-README.txt')
    print('bundle', bundle.name, f'{bundle.stat().st_size/1e6:.1f} MB', sha(bundle))

if __name__ == '__main__':
    main()
