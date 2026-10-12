#!/usr/bin/env python3
"""Linux NAS preflight; standard library only, read-only unless explicitly opted in."""
import argparse
import ctypes
import errno
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tempfile


def entry(check, evidence, result, **details):
    return dict(check=check, evidence=evidence, result=result, **details)


def mount_for(path):
    # 只读挂载元数据，不列举下载或媒体内容，也不输出挂载源/凭据。
    # 按打开目录的真实 mount ID 匹配；路径前缀在沙箱/bind mount 下可能误判。
    try:
        fd = os.open(path, os.O_PATH | os.O_DIRECTORY | os.O_CLOEXEC)
        try:
            info = Path(f"/proc/self/fdinfo/{fd}").read_text().splitlines()
            mount_id = next(line.split()[1] for line in info if line.startswith("mnt_id:"))
        finally:
            os.close(fd)
        for line in Path("/proc/self/mountinfo").read_text().splitlines():
            left, right = line.split(" - ", 1)
            fields, tail = left.split(), right.split()
            if fields[0] == mount_id:
                return dict(
                    mount_id=fields[0], filesystem=tail[0],
                    readonly="ro" in fields[5].split(",") or "ro" in tail[2].split(","))
    except (OSError, StopIteration, ValueError, IndexError):
        pass
    return None


def check_openat2(path):
    # 437 是这些 Linux 架构的 openat2 号；未知架构不猜 syscall。
    if platform.machine().lower() not in {"x86_64", "amd64", "aarch64", "arm64", "armv7l", "i386", "i686", "riscv64"}:
        return entry("openat2", "unverified", "unknown", reason="unsupported_probe_architecture")

    class OpenHow(ctypes.Structure):
        _fields_ = [("flags", ctypes.c_uint64), ("mode", ctypes.c_uint64), ("resolve", ctypes.c_uint64)]

    fd = os.open(path, os.O_PATH | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        libc = ctypes.CDLL(None, use_errno=True)
        # 与服务相同的 BENEATH | NO_SYMLINKS，只打开目录自身，绝不写入。
        how = OpenHow(os.O_PATH | os.O_DIRECTORY | os.O_CLOEXEC, 0, 0x08 | 0x04)
        opened = libc.syscall(ctypes.c_long(437), ctypes.c_int(fd), ctypes.c_char_p(b"."),
                              ctypes.byref(how), ctypes.c_size_t(ctypes.sizeof(how)))
        if opened < 0:
            return entry("openat2", "confirmed", "fail", error=errno.errorcode.get(ctypes.get_errno(), "OS_ERROR"))
        os.close(opened)
        proc_visible = Path(f"/proc/self/fd/{fd}").is_dir()
        return entry("openat2", "confirmed", "pass" if proc_visible else "fail", proc_self_fd=proc_visible,
                     scope="current_process_namespace")
    finally:
        os.close(fd)


def docker_check():
    if not shutil.which("docker"):
        return entry("docker", "confirmed", "fail", reason="cli_missing")
    try:
        # 不输出环境变量、context、认证配置、原始错误或 docker inspect。
        run = subprocess.run(["docker", "version", "--format", "{{json .Server}}"],
                             capture_output=True, text=True, timeout=10)
        if run.returncode:
            return entry("docker", "confirmed", "fail", reason="daemon_not_visible")
        server = json.loads(run.stdout)
        if not isinstance(server, dict):
            return entry("docker", "unverified", "unknown", reason="server_version_unavailable")
        return entry("docker", "confirmed", "pass", server_version=server.get("Version"),
                     server_os=server.get("Os"), server_arch=server.get("Arch"),
                     scope="currently_configured_docker_daemon")
    except (OSError, subprocess.TimeoutExpired, ValueError):
        return entry("docker", "unverified", "unknown", reason="version_query_failed_or_timed_out")


def probe_hardlink(directory, roots):
    # 写探针仅限明确 opt-in 的现有空隔离目录，拒绝目标树及其祖先。
    raw = Path(directory)
    if not raw.is_absolute() or raw.is_symlink() or not raw.is_dir():
        return entry("hardlink_probe", "confirmed", "fail", reason="probe_requires_absolute_existing_directory")
    path = raw.resolve()
    for root in roots:
        if path == root or path in root.parents or root in path.parents:
            return entry("hardlink_probe", "confirmed", "fail", reason="probe_overlaps_target_tree")
    with os.scandir(path) as items:
        if next(items, None) is not None:
            return entry("hardlink_probe", "confirmed", "fail", reason="probe_directory_not_empty")
    # 仅清理自己创建的两个文件及私有子目录，不删除用户文件或递归清理。
    private = Path(tempfile.mkdtemp(prefix="asuka-nas-probe-", dir=path))
    fd = os.open(private, os.O_RDONLY | os.O_DIRECTORY | os.O_CLOEXEC)
    try:
        source = os.open("source", os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600, dir_fd=fd)
        try:
            os.write(source, b"asuka-nas isolated hardlink probe\n")
            os.fsync(source)
        finally:
            os.close(source)
        os.link("source", "linked", src_dir_fd=fd, dst_dir_fd=fd, follow_symlinks=False)
        os.fsync(fd)
        src = os.stat("source", dir_fd=fd, follow_symlinks=False)
        dst = os.stat("linked", dir_fd=fd, follow_symlinks=False)
        same = (src.st_dev, src.st_ino) == (dst.st_dev, dst.st_ino) and src.st_nlink == 2
        return entry("hardlink_probe", "confirmed", "pass" if same else "fail",
                     scope="isolated_probe_directory_only", source_retained_during_probe=True,
                     device=src.st_dev, reason="production_cross_mount_link_still_unverified")
    finally:
        for name in ("linked", "source"):
            try:
                os.unlink(name, dir_fd=fd)
            except FileNotFoundError:
                pass
        os.close(fd)
        private.rmdir()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--downloads", required=True, help="existing absolute downloads root")
    parser.add_argument("--media", required=True, help="existing absolute media root")
    parser.add_argument("--state", required=True, help="existing absolute database parent directory")
    parser.add_argument("--runtime-uid", type=int, default=65532, help="intended service UID; no impersonation")
    parser.add_argument("--write-probe", action="store_true", help="explicitly allow isolated temporary writes")
    parser.add_argument("--probe-dir", help="existing empty isolated directory, separate from target trees")
    args = parser.parse_args()
    if args.write_probe != bool(args.probe_dir):
        parser.error("--write-probe and --probe-dir must be supplied together")
    if args.runtime_uid < 0:
        parser.error("--runtime-uid must be nonnegative")
    checks = [entry("system", "confirmed", "pass" if platform.system() == "Linux" else "fail",
                    os=platform.system(), architecture=platform.machine(), kernel=platform.release()),
              entry("identity", "confirmed", "pass", uid=os.geteuid(), gid=os.getegid(),
                    runtime_uid=args.runtime_uid, matches_runtime_uid=os.geteuid() == args.runtime_uid)]
    if platform.system() != "Linux":
        print(json.dumps(dict(mode="read-only", checks=checks), ensure_ascii=False, indent=2))
        return 1
    checks.append(docker_check())
    roots, stats, mounts = {}, {}, {}
    for label in ("downloads", "media", "state"):
        raw = Path(getattr(args, label))
        try:
            if not raw.is_absolute() or not raw.is_dir():
                checks.append(entry(label, "confirmed", "fail", reason="requires_absolute_existing_directory"))
                continue
            if any(part.is_symlink() for part in (raw, *raw.parents)):
                checks.append(entry(label, "confirmed", "fail", reason="symlink_in_target_root"))
                continue
            path = raw.resolve(strict=True)
            roots[label] = path
            stats[label] = path.stat()
            mounts[label] = mount_for(path)
            flags = os.R_OK | os.X_OK if label == "downloads" else os.W_OK | os.X_OK
            access = os.access(path, flags, effective_ids=True)
            if label != "downloads" and mounts[label] and mounts[label]["readonly"]:
                access = False
            checks.append(entry(label, "confirmed", "pass" if access else "fail",
                                path=str(path), device=stats[label].st_dev, inode=stats[label].st_ino,
                                owner_uid=stats[label].st_uid, mode=oct(stats[label].st_mode & 0o777),
                                access_for_current_uid=access, mount=mounts[label]))
            checks.append(entry(label + "_runtime_permissions", "inferred" if os.geteuid() == args.runtime_uid else "unverified",
                                "unknown", reason="access_flags_do_not_prove_operations_or_existing_file_permissions",
                                tested_uid=os.geteuid(), intended_uid=args.runtime_uid))
            if mounts[label] is None:
                checks.append(entry(label + "_mount", "unverified", "unknown", reason="mount_id_not_visible"))
        except OSError as exc:
            checks.append(entry(label, "confirmed", "fail", error=errno.errorcode.get(exc.errno, "OS_ERROR")))
    if "downloads" in roots and "media" in roots:
        src, dst = stats["downloads"], stats["media"]
        distinct = ((src.st_dev, src.st_ino) != (dst.st_dev, dst.st_ino)
                    and roots["downloads"] not in roots["media"].parents
                    and roots["media"] not in roots["downloads"].parents)
        checks.append(entry("distinct_roots", "confirmed", "pass" if distinct else "fail"))
        same = src.st_dev == dst.st_dev
        checks.append(entry("hardlink_feasibility", "inferred", "unknown" if same and distinct else "fail",
                            same_device=same, same_mount=(mounts["downloads"]["mount_id"] == mounts["media"]["mount_id"]
                                if mounts["downloads"] and mounts["media"] else None),
                            reason="same_device_is_necessary_not_sufficient_no_link_attempted"))
        try:
            checks.append(check_openat2(roots["downloads"]))
        except OSError as exc:
            checks.append(entry("openat2", "confirmed", "fail", error=errno.errorcode.get(exc.errno, "OS_ERROR")))
    try:
        policy = Path("/proc/sys/fs/protected_hardlinks").read_text().strip()
        checks.append(entry("protected_hardlinks", "confirmed", "pass", enabled=policy == "1"))
    except OSError:
        checks.append(entry("protected_hardlinks", "unverified", "unknown", reason="not_readable"))
    checks.append(entry("service_integration", "unverified", "unknown",
                        reason="container_mount_identity_qb_save_path_emby_path_source_ownership_and_fsync_not_tested"))
    if "state" in mounts:
        filesystem = mounts["state"]["filesystem"] if mounts["state"] else None
        remote = filesystem in {"nfs", "nfs4", "cifs", "smb3", "fuse.sshfs"}
        checks.append(entry("sqlite_state_filesystem", "inferred" if filesystem else "unverified",
                            "fail" if remote else "unknown", filesystem=filesystem,
                            reason="requires_local_persistent_storage_and_separate_wal_durability_validation"))
    if args.write_probe:
        if len(roots) != 3:
            checks.append(entry("hardlink_probe", "confirmed", "fail", reason="all_target_roots_must_be_valid"))
        else:
            try:
                checks.append(probe_hardlink(args.probe_dir, list(roots.values())))
            except OSError as exc:
                checks.append(entry("hardlink_probe", "confirmed", "fail", error=errno.errorcode.get(exc.errno, "OS_ERROR")))
    else:
        checks.append(entry("hardlink_probe", "unverified", "unknown", reason="default_read_only_no_link_attempted"))
    failed = any(v["result"] == "fail" for v in checks)
    print(json.dumps(dict(mode="isolated-write-probe" if args.write_probe else "read-only",
                         deployment_ready=False, checks=checks), ensure_ascii=False, indent=2))
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
