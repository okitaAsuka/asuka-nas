"""Safety boundaries for the preflight, using only isolated synthetic directories."""
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).resolve().parents[1] / "scripts" / "nas-preflight.py"


@unittest.skipUnless(platform.system() == "Linux", "NAS preflight is Linux-only")
class PreflightSafety(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="nas-preflight-test-")
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.roots = {label: self.base / label for label in ("downloads", "media", "state")}
        for path in self.roots.values():
            path.mkdir()
            (path / "do-not-touch").write_bytes(b"fixture-secret-never-print")
        self.probe = self.base / "probe"
        self.probe.mkdir()
        self.bin = self.base / "bin"
        self.bin.mkdir()
        docker = self.bin / "docker"
        docker.write_text(f"#!{sys.executable}\nimport json\nprint(json.dumps({{'Version':'fixture', 'Os':'linux', 'Arch':'amd64', 'IgnoredSecret':'fixture-secret-never-print'}}))\n")
        docker.chmod(0o700)

    def snapshot(self):
        # 仅对本测试的合成目录取快照，生产工具不会遍历目标内容。
        return {str(p): (p.stat().st_ino, p.stat().st_mtime_ns, p.read_bytes() if p.is_file() else None)
                for root in self.roots.values() for p in [root, *root.rglob("*")]}

    def run_tool(self, *extra, replacements=None):
        roots = self.roots | (replacements or {})
        command = [sys.executable, "-B", str(SCRIPT)]
        for label, path in roots.items():
            command.extend(["--" + label, str(path)])
        result = subprocess.run(command + list(extra), capture_output=True, text=True,
                                env=os.environ | {"PATH": str(self.bin)}, timeout=20)
        self.assertNotIn("fixture-secret-never-print", result.stdout + result.stderr)
        return result, json.loads(result.stdout) if result.stdout else None

    @staticmethod
    def check(report, name):
        return next(c for c in report["checks"] if c["check"] == name)

    def test_default_does_not_write_or_claim_hardlink_success(self):
        before = self.snapshot()
        result, report = self.run_tool("--runtime-uid", str(os.geteuid() + 1))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.snapshot(), before)
        self.assertEqual(list(self.probe.iterdir()), [])
        self.assertEqual(report["mode"], "read-only")
        self.assertFalse(report["deployment_ready"])
        self.assertEqual(self.check(report, "hardlink_probe")["evidence"], "unverified")
        self.assertEqual(self.check(report, "hardlink_feasibility")["result"], "unknown")
        self.assertEqual(self.check(report, "media_runtime_permissions")["evidence"], "unverified")

    def test_opt_in_links_only_in_isolated_directory_and_cleans_up(self):
        before = self.snapshot()
        result, report = self.run_tool("--write-probe", "--probe-dir", str(self.probe))
        self.assertEqual(result.returncode, 0, result.stderr)
        proof = self.check(report, "hardlink_probe")
        self.assertEqual(proof["result"], "pass")
        self.assertEqual(proof["scope"], "isolated_probe_directory_only")
        self.assertTrue(proof["source_retained_during_probe"])
        self.assertFalse(report["deployment_ready"])
        self.assertEqual(list(self.probe.iterdir()), [])
        self.assertEqual(self.snapshot(), before)

    def test_partial_opt_in_is_rejected(self):
        for extra in (("--write-probe",), ("--probe-dir", str(self.probe))):
            result, _ = self.run_tool(*extra)
            self.assertEqual(result.returncode, 2)
        self.assertEqual(list(self.probe.iterdir()), [])

    def test_nonempty_missing_and_target_probe_are_rejected_without_writes(self):
        sentinel = self.probe / "keep"
        sentinel.write_bytes(b"keep")
        before = self.snapshot()
        for path, reason in ((self.probe, "probe_directory_not_empty"),
                             (self.base / "missing", "probe_requires_absolute_existing_directory"),
                             (self.roots["media"], "probe_overlaps_target_tree"),
                             (self.base, "probe_overlaps_target_tree")):
            result, report = self.run_tool("--write-probe", "--probe-dir", str(path))
            self.assertEqual(result.returncode, 1)
            self.assertEqual(self.check(report, "hardlink_probe")["reason"], reason)
        self.assertEqual(sentinel.read_bytes(), b"keep")
        self.assertEqual(self.snapshot(), before)
        self.assertFalse((self.base / "missing").exists())

    def test_symlink_root_and_identical_roots_are_rejected(self):
        link = self.base / "linked-root"
        link.symlink_to(self.roots["downloads"], target_is_directory=True)
        result, report = self.run_tool(replacements={"downloads": link})
        self.assertEqual(result.returncode, 1)
        self.assertEqual(self.check(report, "downloads")["reason"], "symlink_in_target_root")
        result, report = self.run_tool(replacements={"media": self.roots["downloads"]})
        self.assertEqual(result.returncode, 1)
        self.assertEqual(self.check(report, "distinct_roots")["result"], "fail")


if __name__ == "__main__":
    unittest.main()
