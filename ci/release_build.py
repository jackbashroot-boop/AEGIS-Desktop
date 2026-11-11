#!/usr/bin/env python3
"""CI release assembly (GitHub Actions). Mirrors the local
aegis-desktop/build_installers.py but runs on a plain checkout:
builds the suite payloads from whatever device-package zips are in
release-assets/ (the workflow drops the fresh PHOENIX zip there;
drop other programs' zips into release-assets/ manually when they
change), compiles the stdlib-only installer for both OSes, and
writes latest.json + phoenix-latest.json next to the binaries.
Usage: release_build.py v1.2.13
"""
import hashlib, json, os, subprocess, sys, zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ASSETS = ROOT / 'release-assets'
DESK = ROOT / 'aegis-desktop'
TAG = sys.argv[1] if len(sys.argv) > 1 else 'v0.0.0'
SUITE = TAG.lstrip('v')

def sha(p): return hashlib.sha256(Path(p).read_bytes()).hexdigest()

def app_version():
    for ln in (DESK / 'app/config.go').read_text().splitlines():
        if 'appVersion' in ln and '=' in ln:
            return ln.split('"')[1]
    raise SystemExit('appVersion not found')

def fw_version():
    for ln in (DESK.parent / 'aegis-phoenix-firmware/src/main.cpp').read_text().splitlines():
        if 'FW_VER_NUM' in ln and '"' in ln:
            return ln.split('"')[1]
    raise SystemExit('FW_VER_NUM not found')

def main():
    ASSETS.mkdir(exist_ok=True)
    comp_ver, fw_ver = app_version(), fw_version()
    # PHOENIX SD package zip for the suite payload
    phx = ASSETS / f'Aegis-Phoenix-v{fw_ver}-SD.zip'
    with zipfile.ZipFile(phx, 'w', zipfile.ZIP_DEFLATED) as z:
        z.write(ASSETS / 'aegis-phoenix-adv.bin', 'aegis-phoenix-adv.bin')
        sdroot = ROOT / 'aegis-phoenix-firmware/sdcard'
        if sdroot.exists():
            for f in sorted(sdroot.rglob('*')):
                if f.is_file():
                    z.write(f, f.relative_to(sdroot).as_posix())
    src = ROOT / 'ci-installer-src'
    src.mkdir(exist_ok=True)
    for f in (DESK / 'installer').glob('*.go'):
        (src / f.name).write_bytes(f.read_bytes())
    (src / 'go.mod').write_text('module aegissuite\n\ngo 1.24\n')
    comp = {'linux': ASSETS / 'AegisCompanion', 'windows': ASSETS / 'AegisCompanion.exe'}
    for osname, osbin in comp.items():
        # payload zip (installer expects them in its current dir at build time
        # via go:embed files payload-linux.zip / payload-windows.zip)
        if not osbin.exists():
            print('skip payload', osname, '(no companion binary)')
            continue
        import io
        buf = io.BytesIO()
        files = []
        with zipfile.ZipFile(buf, 'w', zipfile.ZIP_DEFLATED) as z:
            arc = osbin.name
            zi = zipfile.ZipInfo(arc)
            zi.external_attr = 0o755 << 16  # the app binary must stay executable
            z.writestr(zi, osbin.read_bytes())
            files.append(arc)
            for zpath in sorted(ASSETS.glob('Aegis-*-SD.zip')) + sorted(ASSETS.glob('Aegis-*-CYD.zip')):
                z.write(zpath, f'Suite/{zpath.name}')
                files.append(f'Suite/{zpath.name}')
        entries = []
        with zipfile.ZipFile(buf) as z:
            pass
        # manifest with hashes (re-open the bytes we just wrote)
        import hashlib as hl
        tmp = buf.getvalue()
        manifest = {'suite': SUITE, 'companion': comp_ver, 'files': []}
        with zipfile.ZipFile(io.BytesIO(tmp)) as z:
            for name in z.namelist():
                d = z.read(name)
                manifest['files'].append({'path': name, 'sha256': hl.sha256(d).hexdigest(), 'size': len(d)})
        # rewrite with manifest appended
        buf2 = io.BytesIO()
        with zipfile.ZipFile(buf2, 'w', zipfile.ZIP_DEFLATED) as z:
            z.writestr('suite-manifest.json', json.dumps(manifest, indent=2))
            with zipfile.ZipFile(io.BytesIO(tmp)) as zin:
                for name in zin.namelist():
                    z.writestr(name, zin.read(name))
        (src / f'payload-{osname}.zip').write_bytes(buf2.getvalue())
    env = dict(os.environ, CGO_ENABLED='0')
    for goos, name in [('linux', 'Aegis-Suite-Setup-Linux'), ('windows', 'Aegis-Suite-Setup-Windows.exe')]:
        if not (src / f'payload-{goos}.zip').exists():
            print('skip installer', name)
            continue
        e = dict(env, GOOS=goos, GOARCH='amd64')
        subprocess.run(['go', 'build', '-ldflags', '-s -w', '-o', str(ASSETS / name), '.'], cwd=src, env=e, check=True)
        print('built', name, (ASSETS / name).stat().st_size)
    # Feed contract (fixed in 1.2.22): "version"/"companion" carry the
    # COMPANION version - the updater compares it against its own
    # 1.8.x. Suite 1.2.15-1.2.21 feeds put the suite 1.2.x there, which
    # always sorted older, so no Companion ever saw an update. The
    # suite version rides along in "suite" for display only. Entries
    # carry both "file" and "name" plus the flat legacy keys so every
    # Companion parser back to 1.8.2 can name, download and verify.
    feed = {'version': comp_ver, 'companion': comp_ver, 'suite': SUITE,
            'notes': f'AEGIS Suite {SUITE} (Companion {comp_ver}, PHOENIX {fw_ver})',
            'files': {
                'linux':   {'file': 'Aegis-Suite-Setup-Linux', 'name': 'Aegis-Suite-Setup-Linux', 'sha256': sha(ASSETS / 'Aegis-Suite-Setup-Linux')},
                'windows': {'file': 'Aegis-Suite-Setup-Windows.exe', 'name': 'Aegis-Suite-Setup-Windows.exe', 'sha256': sha(ASSETS / 'Aegis-Suite-Setup-Windows.exe')},
            },
            'file_linux': 'Aegis-Suite-Setup-Linux',
            'file_windows': 'Aegis-Suite-Setup-Windows.exe'}
    for goos, key in [('linux', 'linux'), ('windows', 'windows')]:
        f = ASSETS / feed['files'][key]['file']
        if f.exists():
            feed['files'][key]['size'] = f.stat().st_size
            feed[key] = {'file': f.name, 'name': f.name, 'sha256': sha(f), 'size': f.stat().st_size}
            feed[f'sha256_{key}'] = sha(f)
    (ASSETS / 'latest.json').write_text(json.dumps(feed, indent=2))
    phx_feed = {'version': fw_ver, 'file': 'aegis-phoenix-adv.bin',
                'sha256': sha(ASSETS / 'aegis-phoenix-adv.bin'),
                'size': (ASSETS / 'aegis-phoenix-adv.bin').stat().st_size,
                'notes': f'PHOENIX {fw_ver} (CI release {TAG})'}
    (ASSETS / 'phoenix-latest.json').write_text(json.dumps(phx_feed, indent=2))
    print('feeds written: suite', SUITE, 'companion', comp_ver, 'phoenix', fw_ver)

if __name__ == '__main__':
    main()
