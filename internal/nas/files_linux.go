//go:build linux

package nas

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
)

// Files 持有可信根目录 fd，拒绝任何符号链接。
type Files struct{ source, target int }

// OpenFiles 要求两个根已存在；不自动创建部署目录。
func OpenFiles(source, target string) (*Files, error) {
	open := func(p string) (int, error) {
		return unix.Openat2(unix.AT_FDCWD, p, &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS})
	}
	s, e := open(source)
	if e != nil {
		return nil, errors.New("invalid_download_root")
	}
	d, e := open(target)
	if e != nil {
		unix.Close(s)
		return nil, errors.New("invalid_library_root")
	}
	var ss, ds unix.Stat_t
	if unix.Fstat(s, &ss) != nil || unix.Fstat(d, &ds) != nil {
		unix.Close(s)
		unix.Close(d)
		return nil, errors.New("root_stat_failed")
	}
	if ss.Dev == ds.Dev && ss.Ino == ds.Ino {
		unix.Close(s)
		unix.Close(d)
		return nil, errors.New("overlapping_roots")
	}
	if ss.Dev != ds.Dev {
		unix.Close(s)
		unix.Close(d)
		return nil, errors.New("cross_device")
	}
	return &Files{s, d}, nil
}

// Close 释放目录句柄。
func (f *Files) Close() { unix.Close(f.source); unix.Close(f.target) }

// local 拒绝规范化掩盖的 .. 和绝对路径，而不是先清理再信任。
func local(p string) bool {
	return filepath.IsLocal(p) && p != "." && path.Clean(p) == p && !strings.Contains(p, "\\") && !strings.ContainsAny(p, "\x00\r\n") && len(p) < 4096
}

func openBeneath(root int, p string, flags uint64) (int, error) {
	if !local(p) {
		return -1, errors.New("unsafe_path")
	}
	fd, e := unix.Openat2(root, p, &unix.OpenHow{Flags: flags | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if e != nil {
		return -1, errors.New("unsafe_or_missing_path")
	}
	return fd, nil
}

// fingerprint 包含内容和 inode 身份；执行前后检查元数据，拒绝变化的文件。
func fingerprint(ctx context.Context, fd int) (string, error) {
	if ctx.Err() != nil {
		return "", errors.New("operation_cancelled")
	}
	var before, after unix.Stat_t
	if e := unix.Fstat(fd, &before); e != nil || before.Mode&unix.S_IFMT != unix.S_IFREG {
		return "", errors.New("not_regular_file")
	}
	dup, e := unix.Dup(fd)
	if e != nil {
		return "", errors.New("file_read_failed")
	}
	file := os.NewFile(uintptr(dup), "source")
	defer file.Close()
	if _, e = file.Seek(0, 0); e != nil {
		return "", errors.New("file_read_failed")
	}
	h := sha256.New()
	if _, e = io.Copy(h, contextReader{ctx, file}); e != nil {
		if ctx.Err() != nil {
			return "", errors.New("operation_cancelled")
		}
		return "", errors.New("file_read_failed")
	}
	if unix.Fstat(fd, &after) != nil || before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return "", errors.New("file_changed")
	}
	return fmt.Sprintf("%x:%d:%d:%d:%d:%d", h.Sum(nil), before.Dev, before.Ino, before.Size, before.Mtim.Sec, before.Mtim.Nsec), nil
}

// Fingerprint 从固定根目录打开文件，不跟随任何 symlink。
func (f *Files) Fingerprint(p string) (string, error) {
	return f.FingerprintContext(context.Background(), p)
}

// FingerprintContext 允许 daemon 退出时中断大文件哈希。
func (f *Files) FingerprintContext(ctx context.Context, p string) (string, error) {
	fd, e := openBeneath(f.source, p, unix.O_RDONLY|unix.O_NONBLOCK)
	if e != nil {
		return "", e
	}
	defer unix.Close(fd)
	return fingerprint(ctx, fd)
}

// parent 逐级 mkdirat/openat2，已存在 symlink 不能成为目标父目录。
func (f *Files) parent(p string) (int, string, error) {
	if !local(p) {
		return -1, "", errors.New("unsafe_path")
	}
	parts := strings.Split(p, "/")
	fd, e := unix.Dup(f.target)
	if e != nil {
		return -1, "", errors.New("target_open_failed")
	}
	for _, part := range parts[:len(parts)-1] {
		if e = unix.Mkdirat(fd, part, 0750); e != nil && !errors.Is(e, unix.EEXIST) {
			unix.Close(fd)
			return -1, "", errors.New("target_directory_failed")
		}
		if e = syncDirectory(fd); e != nil {
			unix.Close(fd)
			return -1, "", e
		}
		next, e := openBeneath(fd, part, unix.O_PATH|unix.O_DIRECTORY)
		unix.Close(fd)
		if e != nil {
			return -1, "", e
		}
		fd = next
	}
	return fd, parts[len(parts)-1], nil
}

// Link 不覆盖、不删除、不复制；同 inode 仅代表幂等重放成功。
func (f *Files) Link(p PlannedFile) error { return f.LinkContext(context.Background(), p) }

// LinkContext 每个不可逆效果前检查取消；已创建链接保留供下次恢复。
func (f *Files) LinkContext(ctx context.Context, p PlannedFile) error {
	src, e := openBeneath(f.source, p.Source, unix.O_RDONLY|unix.O_NONBLOCK)
	if e != nil {
		return e
	}
	defer unix.Close(src)
	fp, e := fingerprint(ctx, src)
	if e != nil {
		return e
	}
	if fp != p.Fingerprint {
		return errors.New("fingerprint_changed")
	}
	dst, name, e := f.parent(p.Target)
	if e != nil {
		return e
	}
	defer unix.Close(dst)
	if ctx.Err() != nil {
		return errors.New("operation_cancelled")
	}
	if unix.Fsync(src) != nil {
		return errors.New("file_sync_failed")
	}
	// /proc/self/fd 仅指向已验证的源 fd；AT_SYMLINK_FOLLOW 不作用于用户输入。
	e = unix.Linkat(unix.AT_FDCWD, fmt.Sprintf("/proc/self/fd/%d", src), dst, name, unix.AT_SYMLINK_FOLLOW)
	if errors.Is(e, unix.EACCES) || errors.Is(e, unix.EPERM) {
		return errors.New("permission_denied")
	}
	if errors.Is(e, unix.EXDEV) {
		return errors.New("cross_device")
	}
	if errors.Is(e, unix.EEXIST) {
		var a, b unix.Stat_t
		if unix.Fstat(src, &a) != nil || unix.Fstatat(dst, name, &b, unix.AT_SYMLINK_NOFOLLOW) != nil || a.Dev != b.Dev || a.Ino != b.Ino || b.Mode&unix.S_IFMT != unix.S_IFREG {
			return errors.New("target_conflict")
		}
	} else if e != nil {
		return errors.New("link_failed")
	}
	fp, e = fingerprint(ctx, src)
	if e != nil || fp != p.Fingerprint {
		return errors.New("fingerprint_changed")
	}
	return syncDirectory(dst)
}

// syncDirectory 同步每层父目录，避免 imported 提交后断电丢失新建目录。
func syncDirectory(fd int) error {
	syncfd, e := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return errors.New("directory_sync_failed")
	}
	defer unix.Close(syncfd)
	if unix.Fsync(syncfd) != nil {
		return errors.New("directory_sync_failed")
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

var explicitEpisode = regexp.MustCompile(`^(.+?)[ ._-]+S([0-9]{2})E([0-9]{2})$`)
var subtitleSuffix = regexp.MustCompile(`^(\.[A-Za-z0-9-]+){0,2}\.(srt|ass|ssa|vtt)$`)

// Preview 仅支持无歧义显式季度集数；标题来自文件名而非不可信 RSS。
func (f *Files) Preview(source string, version int) (*Plan, error) {
	return f.PreviewContext(context.Background(), source, version)
}

// PreviewContext 命名和指纹计算共用取消信号。
func (f *Files) PreviewContext(ctx context.Context, source string, version int) (*Plan, error) {
	return f.preview(ctx, source, version, nil)
}

// PreviewManifest 仅从下载器完整清单关联旁车，不扫描共享下载目录内的其它任务。
func (f *Files) PreviewManifest(ctx context.Context, source string, version int, manifest []string) (*Plan, error) {
	return f.preview(ctx, source, version, manifest)
}
func (f *Files) preview(ctx context.Context, source string, version int, manifest []string) (*Plan, error) {
	base := path.Base(source)
	ext := strings.ToLower(path.Ext(base))
	if ext != ".mkv" && ext != ".mp4" {
		return nil, errors.New("needs_confirmation")
	}
	m := explicitEpisode.FindStringSubmatch(strings.TrimSuffix(base, path.Ext(base)))
	if m == nil || m[2] == "00" || m[3] == "00" {
		return nil, errors.New("needs_confirmation")
	}
	title := strings.TrimSpace(strings.ReplaceAll(m[1], ".", " "))
	if title == "" || strings.ContainsAny(title, "/\\\x00\r\n") {
		return nil, errors.New("needs_confirmation")
	}
	target := fmt.Sprintf("%s/Season %s/%s - S%sE%s%s", title, m[2], title, m[2], m[3], ext)
	return f.plan(ctx, source, target, version, manifest)
}

// ProposedPlan 供人工明确命名；仍需 Confirm 绑定版本与完整指纹。
func (f *Files) ProposedPlan(source, target string, version int) (*Plan, error) {
	if strings.ToLower(path.Ext(source)) != strings.ToLower(path.Ext(target)) {
		return nil, errors.New("invalid_target_extension")
	}
	return f.plan(context.Background(), source, target, version, nil)
}

// ProposedManifest 人工命名同样不能跳过合集/字幕清单约束。
func (f *Files) ProposedManifest(ctx context.Context, source, target string, version int, manifest []string) (*Plan, error) {
	if strings.ToLower(path.Ext(source)) != strings.ToLower(path.Ext(target)) {
		return nil, errors.New("invalid_target_extension")
	}
	return f.plan(ctx, source, target, version, manifest)
}

func (f *Files) plan(ctx context.Context, source, target string, version int, manifest []string) (*Plan, error) {
	if !local(source) || !local(target) {
		return nil, errors.New("unsafe_path")
	}
	fp, e := f.FingerprintContext(ctx, source)
	if e != nil {
		return nil, e
	}
	p := &Plan{Version: version, Files: []PlannedFile{{source, target, fp}}}
	if manifest != nil {
		video, reason := singleVideo(manifest)
		if reason != "" {
			return nil, errors.New(reason)
		}
		if video != source {
			return nil, errors.New("unknown_source")
		}
		stem := strings.TrimSuffix(path.Base(source), path.Ext(source))
		targetStem := strings.TrimSuffix(target, path.Ext(target))
		for _, src := range manifest {
			if src == source {
				continue
			}
			name := path.Base(src)
			suffix := strings.TrimPrefix(name, stem)
			if !strings.HasPrefix(name, stem) || !subtitleSuffix.MatchString(suffix) {
				continue
			}
			fp, err := f.FingerprintContext(ctx, src)
			if err != nil {
				return nil, err
			}
			p.Files = append(p.Files, PlannedFile{src, targetStem + suffix, fp})
		}
		return f.validatePlan(p)
	}

	dir := path.Dir(source)
	var fd int
	if dir == "." {
		fd, e = unix.Openat(f.source, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	} else {
		fd, e = openBeneath(f.source, dir, unix.O_RDONLY|unix.O_DIRECTORY)
	}
	if e != nil {
		return nil, errors.New("sidecar_scan_failed")
	}
	df := os.NewFile(uintptr(fd), "sidecars")
	defer df.Close()
	entries, e := df.ReadDir(4097)
	if e != nil && e != io.EOF {
		return nil, errors.New("sidecar_scan_failed")
	}
	if len(entries) > 4096 {
		return nil, errors.New("sidecar_scan_limit")
	}
	stem := strings.TrimSuffix(path.Base(source), path.Ext(source))
	targetStem := strings.TrimSuffix(target, path.Ext(target))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, stem) {
			switch strings.ToLower(path.Ext(name)) {
			case ".srt", ".ass", ".ssa", ".vtt", ".sub", ".idx", ".sup":
				return nil, errors.New("subtitle_mapping_ambiguous")
			}
			continue
		}
		suffix := strings.TrimPrefix(name, stem)
		if !subtitleSuffix.MatchString(suffix) {
			switch strings.ToLower(path.Ext(name)) {
			case ".srt", ".ass", ".ssa", ".vtt", ".sub", ".idx", ".sup":
				return nil, errors.New("subtitle_mapping_ambiguous")
			}
			continue
		}
		src := path.Join(dir, name)
		fp, e = f.FingerprintContext(ctx, src)
		if e != nil {
			return nil, e
		}
		p.Files = append(p.Files, PlannedFile{src, targetStem + suffix, fp})
	}

	return f.validatePlan(p)
}

func (f *Files) validatePlan(p *Plan) (*Plan, error) {
	sort.Slice(p.Files[1:], func(i, j int) bool { return p.Files[i+1].Source < p.Files[j+1].Source })
	for _, file := range p.Files {
		if err := f.CheckTarget(file); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// CheckTarget 预览和确认时不创建目录；已有目标必须是相同 inode。
func (f *Files) CheckTarget(p PlannedFile) error {
	if !local(p.Target) {
		return errors.New("unsafe_path")
	}
	fd, err := unix.Openat2(f.target, p.Target, &unix.OpenHow{Flags: unix.O_PATH | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS})
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return errors.New("unsafe_target")
	}
	defer unix.Close(fd)
	src, err := openBeneath(f.source, p.Source, unix.O_PATH)
	if err != nil {
		return err
	}
	defer unix.Close(src)
	var a, b unix.Stat_t
	if unix.Fstat(src, &a) != nil || unix.Fstat(fd, &b) != nil || a.Dev != b.Dev || a.Ino != b.Ino || b.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("target_conflict")
	}
	return nil
}
