#!/usr/bin/env python3
"""本机离线交付：只归档显式模板及 docker save，不发布或部署。"""
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]


def run(args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def capture(args):
    return subprocess.check_output(args, text=True).strip()


def package(root=ROOT, env=os.environ):
    version = env.get('VERSION') or capture(['git', '-C', str(root), 'rev-parse', '--short=12', 'HEAD'])
    arch = env.get('ARCH') or 'amd64'
    if not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}', version):
        raise ValueError('VERSION must be a safe Docker tag (1-64 characters)')
    if arch not in ('amd64', 'arm64'):
        raise ValueError('ARCH must be amd64 or arm64')
    if arch == 'arm64':
        print('WARNING: arm64 is experimental; runtime/NAS compatibility has not been verified.', flush=True)
    output = Path(env.get('PACKAGE_OUT') or root / 'dist').expanduser().resolve()
    if output == root or output in root.parents or output == Path('/'):
        raise ValueError('PACKAGE_OUT must be a dedicated output directory')
    # 只构建已提交版本，避免脏树与元数据不符；非跟踪秘密永不进入构建上下文。
    run(['git', '-C', str(root), 'diff', '--quiet', 'HEAD'])
    revision = capture(['git', '-C', str(root), 'rev-parse', 'HEAD'])
    name = f'asuka-nas-{version}-linux-{arch}'
    image = f'asuka-nas:{version}-linux-{arch}'
    archive = output / f'{name}.tar.gz'
    checksum = output / f'{name}.tar.gz.sha256'
    output.mkdir(parents=True, exist_ok=True)
    if archive.exists() or checksum.exists():
        raise FileExistsError('package already exists; choose another VERSION or PACKAGE_OUT')
    run(['docker', 'info'], stdout=subprocess.DEVNULL)
    # TemporaryDirectory 仅清理本轮随机目录；不删已有包、镜像、缓存或用户数据。
    with tempfile.TemporaryDirectory(prefix='.nas-package-', dir=output) as tmp:
        tmp = Path(tmp)
        context = tmp / 'context'
        context.mkdir()
        source = tmp / 'source.tar'
        run(['git', '-C', str(root), 'archive', '--format=tar', '-o', str(source), revision,
             'go.mod', 'go.sum', 'cmd', 'internal'])
        with tarfile.open(source) as tar:
            tar.extractall(context, filter='data')
        shutil.copyfile(root / 'Dockerfile', context / 'Dockerfile')
        run(['docker', 'build', '--platform', f'linux/{arch}', '--build-arg', 'TARGETOS=linux',
             '--build-arg', f'TARGETARCH={arch}', '-t', image, str(context)])
        details = json.loads(capture(['docker', 'image', 'inspect', image]))[0]
        if details['Os'] != 'linux' or details['Architecture'] != arch:
            raise ValueError('built image platform mismatch')
        bundle = tmp / name
        bundle.mkdir()
        run(['docker', 'save', '-o', str(bundle / 'image.tar'), image])
        if not (bundle / 'image.tar').stat().st_size:
            raise ValueError('empty Docker image archive')
        for filename in ('compose.yaml', 'config.template.json', 'credentials.env.template'):
            shutil.copyfile(root / 'deploy/offline' / filename, bundle / filename)
        shutil.copyfile(root / 'docs/PACKAGING.md', bundle / 'DEPLOY.md')
        (bundle / '.env').write_text(f'NAS_IMAGE={image}\nNAS_PLATFORM=linux/{arch}\nNAS_STATE_DIR=\nNAS_MEDIA_ROOT=\n')
        (bundle / 'VERSION.json').write_text(json.dumps({
            'version': version, 'revision': revision, 'platform': f'linux/{arch}',
            'image': image, 'image_id': details['Id'], 'arm64_runtime_verified': False,
            'real_nas_verified': False, 'network_candidate': 'linux-host-loopback',
        }, indent=2) + '\n')
        entries = sorted(bundle.iterdir())
        (bundle / 'SHA256SUMS').write_text(''.join(f'{digest(p)}  {p.name}\n' for p in entries))
        staged = tmp / archive.name
        with tarfile.open(staged, 'w:gz') as tar:
            tar.add(bundle, arcname=name)
        staged_checksum = tmp / checksum.name
        staged_checksum.write_text(f'{digest(staged)}  {archive.name}\n')
        # 硬链接独占发布，绝不覆盖既有交付包。
        os.link(staged, archive)
        try:
            os.link(staged_checksum, checksum)
        except BaseException:
            archive.unlink()  # 仅撤销本轮创建的归档。
            raise
    print(f'Package: {archive}\nChecksum: {checksum}')
    return archive


def digest(path):
    h = hashlib.sha256()
    with path.open('rb') as file:
        for chunk in iter(lambda: file.read(1024 * 1024), b''):
            h.update(chunk)
    return h.hexdigest()


if __name__ == '__main__':
    try:
        package()
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        raise SystemExit(f'package-nas failed: {error}')
