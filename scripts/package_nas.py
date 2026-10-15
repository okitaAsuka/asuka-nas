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


def validate_public_templates(root):
    # 交付模板保持关闭且无秘密；实际 TLS/人类 verifier 只由部署者在包外提供。
    config = json.loads((root / 'deploy/offline/config.template.json').read_text())
    if (config.get('management') != {'enabled': False} or config.get('subscriptions') != []
            or config.get('events', {}).get('enabled') is not False
            or config.get('learning', {}).get('enabled') is not False
            or config.get('learning', {}).get('automatic_naming') is not False):
        raise ValueError('public configuration must keep management/events/learning disabled')
    expected = {'ASUKA_NAS_QB_USERNAME', 'ASUKA_NAS_QB_PASSWORD', 'ASUKA_NAS_EMBY_TOKEN',
                'ASUKA_NAS_MANAGEMENT_PASSWORD_HASH'}
    seen = set()
    for line in (root / 'deploy/offline/credentials.env.template').read_text().splitlines():
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        key, separator, value = line.partition('=')
        if not separator or key not in expected or key in seen or value:
            raise ValueError('public credential template must contain only empty approved references')
        seen.add(key)
    if seen != expected:
        raise ValueError('public credential template references are incomplete')


def package(root=ROOT, env=os.environ):
    version = env.get('VERSION') or capture(['git', '-C', str(root), 'rev-parse', '--short=12', 'HEAD'])
    arch = env.get('ARCH') or 'amd64'
    if not re.fullmatch(r'[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}', version):
        raise ValueError('VERSION must be a safe Docker tag (1-64 characters)')
    if arch not in ('amd64', 'arm64'):
        raise ValueError('ARCH must be amd64 or arm64')
    if arch == 'arm64':
        print('WARNING: arm64 is experimental; runtime/NAS compatibility has not been verified.', flush=True)
    module_cache = env.get('MODULE_CACHE')
    if module_cache:
        module_cache = Path(module_cache).expanduser().resolve()
        if not module_cache.is_dir() or module_cache.name != 'download':
            raise ValueError('MODULE_CACHE must be an existing Go pkg/mod/cache/download directory')
    output = Path(env.get('PACKAGE_OUT') or root / 'dist').expanduser().resolve()
    if output == root or output in root.parents or output == Path('/'):
        raise ValueError('PACKAGE_OUT must be a dedicated output directory')
    # 只构建已提交版本，避免脏树与元数据不符；非跟踪秘密永不进入构建上下文。
    run(['git', '-C', str(root), 'diff', '--quiet', 'HEAD'])
    validate_public_templates(root)
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
        dockerfile = 'Dockerfile.offline' if module_cache else 'Dockerfile'
        shutil.copyfile(root / dockerfile, context / 'Dockerfile')
        build = ['docker', 'build', '--platform', f'linux/{arch}', '--build-arg', 'TARGETOS=linux',
                 '--build-arg', f'TARGETARCH={arch}', '-t', image]
        if module_cache:
            build += ['--build-context', f'module-cache={module_cache}']
        run(build + [str(context)])
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
        (bundle / '.env').write_text(f'NAS_IMAGE={image}\nNAS_PLATFORM=linux/{arch}\nNAS_STATE_DIR=\nNAS_MEDIA_ROOT=\nNAS_UID=65532\nNAS_GID=65532\n')
        (bundle / 'VERSION.json').write_text(json.dumps({
            'version': version, 'revision': revision, 'platform': f'linux/{arch}',
            'image': image, 'image_id': details['Id'], 'arm64_runtime_verified': False,
            'real_nas_verified': False, 'network_candidate': 'linux-host-loopback',
        }, indent=2) + '\n')
        entries = sorted(bundle.iterdir())
        (bundle / 'SHA256SUMS').write_text(''.join(f'{digest(p)}  {p.name}\n' for p in entries))
        staged = tmp / archive.name
        with tarfile.open(staged, 'w:gz') as tar:
            tar.add(bundle, arcname=name, filter=archive_mode)
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


def archive_mode(info):
    # 包内只有公开模板和镜像；明确归档权限，使非 root 容器能读取模板。
    # 不更改主机现有文件权限；归档不带本机用户名/UID。
    info.mode = 0o755 if info.isdir() else 0o644
    info.uid = info.gid = 0
    info.uname = info.gname = ''
    return info


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
