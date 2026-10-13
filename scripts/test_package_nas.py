"""无 Docker daemon/真实服务的打包边界与失败清理测试。"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import package_nas as pkg


class PackagingTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='nas package test ')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / 'repo'
        self.root.mkdir()
        for name in ('Dockerfile', 'Dockerfile.offline', 'go.mod', 'go.sum', 'cmd/main.go', 'internal/web/app.js',
                     'deploy/offline/compose.yaml', 'deploy/offline/config.template.json',
                     'deploy/offline/credentials.env.template', 'docs/PACKAGING.md'):
            target = self.root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_text(name)
        self.git('init', '-q')
        self.git('add', '.')
        self.git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid', 'commit', '-qm', 'fixture')
        for name in ('config.json', '.env', 'private.key', 'state.db'):
            (self.root / name).write_text('fixture-secret-must-not-be-packaged')
        self.output = Path(self.temp.name) / 'output with spaces'
        self.env = {'VERSION': 'v1.2.3', 'ARCH': 'amd64', 'PACKAGE_OUT': str(self.output)}
        self.calls = []
        self.fail = None
        self.platform = 'amd64'
        self.empty_save = False
        self.real_run = pkg.run
        self.real_capture = pkg.capture

    def git(self, *args):
        subprocess.run(['git', '-C', str(self.root), *args], check=True, stdout=subprocess.DEVNULL)

    def fake_run(self, args, **kwargs):
        if args[0] != 'docker':
            return self.real_run(args, **kwargs)
        self.calls.append(args)
        if args[1] == self.fail:
            raise subprocess.CalledProcessError(42, args)
        if args[1] == 'build':
            context = Path(args[-1])
            self.assertEqual(sorted(str(p.relative_to(context)) for p in context.rglob('*') if p.is_file()),
                             ['Dockerfile', 'cmd/main.go', 'go.mod', 'go.sum', 'internal/web/app.js'])
        if args[1] == 'save':
            Path(args[3]).write_bytes(b'' if self.empty_save else b'fixture Docker image tar')

    def fake_capture(self, args):
        if args[0] != 'docker':
            return self.real_capture(args)
        return json.dumps([{'Os': 'linux', 'Architecture': self.platform, 'Id': 'sha256:fixture'}])

    def package(self):
        with patch.object(pkg, 'run', self.fake_run), patch.object(pkg, 'capture', self.fake_capture):
            return pkg.package(self.root, self.env)

    def test_bundle_allowlist_metadata_checksums_and_no_overwrite(self):
        archive = self.package()
        checksum = archive.with_suffix(archive.suffix + '.sha256')
        self.assertEqual(checksum.read_text(), f'{pkg.digest(archive)}  {archive.name}\n')
        with tarfile.open(archive) as tar:
            files = {Path(m.name).name: tar.extractfile(m).read() for m in tar.getmembers() if m.isfile()}
        self.assertEqual(set(files), {'image.tar', 'compose.yaml', 'config.template.json',
                                     'credentials.env.template', 'DEPLOY.md', '.env', 'VERSION.json', 'SHA256SUMS'})
        self.assertNotIn(b'fixture-secret-must-not-be-packaged', b''.join(files.values()))
        for line in files['SHA256SUMS'].decode().splitlines():
            expected, name = line.split('  ')
            self.assertEqual(expected, hashlib.sha256(files[name]).hexdigest())
        metadata = json.loads(files['VERSION.json'])
        self.assertEqual(metadata['revision'], self.real_capture(['git', '-C', str(self.root), 'rev-parse', 'HEAD']))
        self.assertEqual(metadata['platform'], 'linux/amd64')
        original = archive.read_bytes()
        with self.assertRaises(FileExistsError):
            self.package()
        self.assertEqual(original, archive.read_bytes())
        self.assertFalse(list(self.output.glob('.nas-package-*')))

    def test_invalid_parameters_fail_before_docker(self):
        for version in ('../escape', 'x;touch evil', '$(echo evil)', '', '-bad', 'v' * 65):
            if not version:
                continue  # 空参数为有意默认值。
            with self.subTest(version=version):
                self.env['VERSION'] = version
                with self.assertRaises(ValueError):
                    self.package()
        self.env['VERSION'] = 'valid'
        for arch in ('windows/amd64', '../arm64', '386'):
            self.env['ARCH'] = arch
            with self.assertRaises(ValueError):
                self.package()
        self.assertEqual(self.calls, [])

    def test_explicit_offline_cache_and_invalid_cache(self):
        self.env['MODULE_CACHE'] = str(self.root)
        with self.assertRaises(ValueError):
            self.package()
        cache = Path(self.temp.name) / 'cache' / 'download'
        cache.mkdir(parents=True)
        self.env['MODULE_CACHE'] = str(cache)
        self.package()
        build = next(call for call in self.calls if call[1] == 'build')
        self.assertIn('--build-context', build)
        self.assertIn(f'module-cache={cache}', build)

    def test_output_parent_rejected(self):
        for path in (self.root, self.root.parent, '/'):
            self.env['PACKAGE_OUT'] = str(path)
            with self.assertRaises(ValueError):
                self.package()
        self.assertEqual(self.calls, [])

    def test_dirty_tracked_tree_rejected(self):
        (self.root / 'go.mod').write_text('dirty')
        with self.assertRaises(subprocess.CalledProcessError):
            self.package()
        self.assertEqual(self.calls, [])

    def test_failure_cleanup_preserves_existing_files(self):
        self.output.mkdir()
        keep = self.output / 'old.tar.gz'
        keep.write_text('keep')
        for step in ('info', 'build', 'save'):
            self.fail = step
            with self.assertRaises(subprocess.CalledProcessError):
                self.package()
            self.assertEqual(list(self.output.iterdir()), [keep])
            self.assertEqual(keep.read_text(), 'keep')

    def test_empty_save_rejected(self):
        self.empty_save = True
        with self.assertRaises(ValueError):
            self.package()
        self.assertFalse(list(self.output.iterdir()))

    def test_wrong_platform_rejected(self):
        self.platform = 'arm64'
        with self.assertRaises(ValueError):
            self.package()
        self.assertFalse(list(self.output.iterdir()))
        self.assertFalse(any(c[1] == 'save' for c in self.calls))

    def test_defaults_and_arm64_marked_unverified(self):
        self.env = {'PACKAGE_OUT': str(self.output), 'ARCH': 'arm64'}
        self.platform = 'arm64'
        archive = self.package()
        with tarfile.open(archive) as tar:
            member = next(m for m in tar.getmembers() if m.name.endswith('/VERSION.json'))
            metadata = json.load(tar.extractfile(member))
        self.assertFalse(metadata['arm64_runtime_verified'])
        self.assertFalse(metadata['real_nas_verified'])
        self.assertTrue(any('linux/arm64' in call for call in self.calls))

    def test_publish_checksum_failure_removes_only_new_archive(self):
        original_link = os.link
        def link(source, target):
            if str(target).endswith('.sha256'):
                raise OSError('fixture publish failure')
            original_link(source, target)
        with patch.object(pkg.os, 'link', link), self.assertRaises(OSError):
            self.package()
        self.assertFalse(list(self.output.iterdir()))


if __name__ == '__main__':
    unittest.main()
