package nas

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const libraryEntryLimit = 512
const libraryNFOLimit = 64 << 10
const libraryByteLimit = 1 << 20
const libraryDepthLimit = 3

// LibraryEvidence 只输出局部证据，不信任 NFO 的外链、提供商 ID 或剧集标题。
type LibraryEvidence struct {
	Source      string `json:"source"`
	Kind        string `json:"kind"`
	DisplayName string `json:"display_name,omitempty"`
	Season      int    `json:"season,omitempty"`
	Episode     int    `json:"episode,omitempty"`
	Absolute    int    `json:"absolute,omitempty"`
}
type LibraryCandidate struct {
	Season      int               `json:"season"`
	Episodes    []int             `json:"episodes"`
	DisplayName string            `json:"display_name"`
	SeriesID    string            `json:"series_id"`
	Sources     []LibraryEvidence `json:"sources"`
	Conflicts   []string          `json:"conflicts"`
	Unknown     []string          `json:"unknown"`
}
type LibraryCandidates struct {
	Directory  string             `json:"directory"`
	Digest     string             `json:"digest"`
	Candidates []LibraryCandidate `json:"candidates"`
}

// discoverLibrary 仅显式选择的目录；限额失败不返回部分候选，也不读取视频内容。
func discoverLibrary(ctx context.Context, root, directory string) (LibraryCandidates, error) {
	result := LibraryCandidates{Directory: directory, Candidates: []LibraryCandidate{}}
	if !local(directory) {
		return result, errors.New("unsafe_path")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rootfd, err := unix.Openat2(unix.AT_FDCWD, root, &unix.OpenHow{Flags: unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC, Resolve: unix.RESOLVE_NO_SYMLINKS})
	if err != nil {
		return result, errors.New("library_unavailable")
	}
	defer unix.Close(rootfd)
	fd, err := openBeneath(rootfd, directory, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return result, errors.New("library_unsafe_path")
	}
	entries, totalBytes := 0, 0
	h := sha256.New()
	evidence := []LibraryEvidence{}
	conflicts := []string{}
	var walk func(int, string, int) error
	walk = func(fd int, relative string, depth int) error {
		df := os.NewFile(uintptr(fd), "library")
		defer df.Close()
		if ctx.Err() != nil {
			return errors.New("library_cancelled")
		}
		var before, after unix.Stat_t
		if unix.Fstat(fd, &before) != nil {
			return errors.New("library_read_failed")
		}
		identity, _ := json.Marshal(struct {
			Directory string
			Stat      libraryIdentity
		}{relative, libraryStat(before)})
		h.Write(identity)
		list, e := df.ReadDir(libraryEntryLimit + 1)
		if e != nil && e != io.EOF {
			return errors.New("library_read_failed")
		}
		entries += len(list)
		if entries > libraryEntryLimit {
			return errors.New("library_limit")
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Name() < list[j].Name() })
		for _, item := range list {
			if ctx.Err() != nil {
				return errors.New("library_cancelled")
			}
			name := item.Name()
			rel := path.Join(relative, name)
			if !local(rel) {
				return errors.New("library_unsafe_path")
			}
			child, e := openBeneath(fd, name, unix.O_PATH)
			if e != nil {
				return errors.New("library_unsafe_path")
			}
			var st unix.Stat_t
			e = unix.Fstat(child, &st)
			unix.Close(child)
			if e != nil {
				return errors.New("library_read_failed")
			}
			// 摘要包含所有目录项身份/时间；旁车变化同样使旧候选失效。
			b, _ := json.Marshal(struct {
				Path string
				Stat libraryIdentity
			}{rel, libraryStat(st)})
			h.Write(b)
			switch st.Mode & unix.S_IFMT {
			case unix.S_IFDIR:
				if depth >= libraryDepthLimit {
					return errors.New("library_limit")
				}
				next, e := openBeneath(fd, name, unix.O_RDONLY|unix.O_DIRECTORY)
				if e != nil {
					return errors.New("library_unsafe_path")
				}
				if e = walk(next, rel, depth+1); e != nil {
					return e
				}
			case unix.S_IFREG:
				ext := strings.ToLower(path.Ext(name))
				if ext == ".nfo" {
					if st.Size > libraryNFOLimit || st.Size < 0 {
						return errors.New("library_limit")
					}
					nfd, e := openBeneath(fd, name, unix.O_RDONLY|unix.O_NONBLOCK)
					if e != nil {
						return errors.New("library_unsafe_path")
					}
					nf := os.NewFile(uintptr(nfd), "nfo")
					var opened, final unix.Stat_t
					e = unix.Fstat(nfd, &opened)
					var data []byte
					if e == nil && libraryStat(opened) == libraryStat(st) {
						data, e = io.ReadAll(io.LimitReader(contextReader{ctx, nf}, libraryNFOLimit+1))
					} else {
						e = errors.New("changed")
					}
					statErr := unix.Fstat(nfd, &final)
					nf.Close()
					if ctx.Err() != nil {
						return errors.New("library_cancelled")
					}
					if e != nil || statErr != nil || libraryStat(final) != libraryStat(st) {
						return errors.New("library_changed")
					}
					totalBytes += len(data)
					if len(data) > libraryNFOLimit || totalBytes > libraryByteLimit {
						return errors.New("library_limit")
					}
					h.Write(data)
					ev, e := parseLibraryNFO(data)
					if e != nil {
						conflicts = append(conflicts, rel+": invalid_nfo")
						continue
					}
					ev.Source = rel
					evidence = append(evidence, ev)
				} else if ext == ".mkv" || ext == ".mp4" {
					stem := strings.TrimSuffix(name, path.Ext(name))
					m := explicitEpisode.FindStringSubmatch(stem)
					if m != nil && m[2] != "00" && m[3] != "00" {
						season, _ := strconv.Atoi(m[2])
						episode, _ := strconv.Atoi(m[3])
						display := strings.TrimSpace(strings.TrimSuffix(m[1], " -"))
						evidence = append(evidence, LibraryEvidence{Source: rel, Kind: "filename", DisplayName: display, Season: season, Episode: episode})
					}
				}
			default:
				return errors.New("library_unsafe_path")
			}
		}
		if unix.Fstat(fd, &after) != nil || libraryStat(before) != libraryStat(after) {
			return errors.New("library_changed")
		}
		return nil
	}
	if err = walk(fd, "", 0); err != nil {
		return result, err
	}
	result.Digest = fmt.Sprintf("%x", h.Sum(nil))
	// 同 stem 的 NFO/视频季集必须一致；不能以目录 Season NN 单独确定集序。
	for _, a := range evidence {
		for _, b := range evidence {
			if a.Kind == "filename" && b.Kind == "episode_nfo" && strings.TrimSuffix(a.Source, path.Ext(a.Source)) == strings.TrimSuffix(b.Source, path.Ext(b.Source)) && (a.Season != b.Season || a.Episode != b.Episode) {
				conflicts = append(conflicts, a.Source+": episode_conflict")
			}
		}
	}
	names := map[string]bool{}
	seasons := map[int]bool{}
	for _, ev := range evidence {
		if ev.DisplayName != "" {
			names[ev.DisplayName] = true
		}
		if ev.Season > 0 && ev.Episode > 0 {
			seasons[ev.Season] = true
		}
	}
	display := ""
	if len(names) == 1 {
		for n := range names {
			display = n
		}
	} else if len(names) > 1 {
		conflicts = append(conflicts, "display_name_conflict")
	}
	if len(seasons) == 0 {
		seasons[0] = true
	}
	ordered := []int{}
	for season := range seasons {
		ordered = append(ordered, season)
	}
	sort.Ints(ordered)
	sort.Strings(conflicts)
	for _, season := range ordered {
		candidate := LibraryCandidate{Season: season, DisplayName: display, SeriesID: "library-" + digestString(directory)[:16], Sources: []LibraryEvidence{}, Conflicts: append([]string{}, conflicts...), Episodes: []int{}, Unknown: []string{"subscription_and_alias_require_selection", "arc_season_requires_confirmation", "episode_order_requires_confirmation"}}
		eps := map[int]bool{}
		absolute := map[int]int{}
		relative := map[int]int{}
		for _, ev := range evidence {
			if ev.Season == season || ev.Kind == "series_nfo" {
				candidate.Sources = append(candidate.Sources, ev)
			}
			if ev.Season == season && ev.Episode > 0 {
				eps[ev.Episode] = true
				if ev.Absolute > 0 {
					if (absolute[ev.Absolute] != 0 && absolute[ev.Absolute] != ev.Episode) || (relative[ev.Episode] != 0 && relative[ev.Episode] != ev.Absolute) {
						candidate.Conflicts = append(candidate.Conflicts, "absolute_episode_conflict")
					}
					absolute[ev.Absolute] = ev.Episode
					relative[ev.Episode] = ev.Absolute
				}
			}
		}
		for ep := range eps {
			candidate.Episodes = append(candidate.Episodes, ep)
		}
		sort.Ints(candidate.Episodes)
		if season == 0 {
			candidate.Unknown = append(candidate.Unknown, "season_unknown")
		}
		if display == "" {
			candidate.Unknown = append(candidate.Unknown, "display_name_unknown")
		}
		if len(absolute) == 0 {
			candidate.Unknown = append(candidate.Unknown, "absolute_mapping_unknown")
		}
		result.Candidates = append(result.Candidates, candidate)
	}
	return result, nil
}

// 标准 XML decoder 不解析外部实体；额外拒绝 DTD/指令与嵌套、重复字段。
func parseLibraryNFO(data []byte) (LibraryEvidence, error) {
	ev := LibraryEvidence{}
	d := xml.NewDecoder(bytes.NewReader(data))
	depth, tokens := 0, 0
	root, field := "", ""
	values := map[string]string{}
	closed := false
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return ev, errors.New("invalid_nfo")
		}
		tokens++
		if tokens > 4096 {
			return ev, errors.New("invalid_nfo")
		}
		switch t := token.(type) {
		case xml.Directive:
			return ev, errors.New("invalid_nfo")
		case xml.ProcInst:
			if t.Target != "xml" || tokens != 1 {
				return ev, errors.New("invalid_nfo")
			}
		case xml.StartElement:
			depth++
			if depth > 16 || closed || t.Name.Space != "" {
				return ev, errors.New("invalid_nfo")
			}
			if depth == 1 {
				root = t.Name.Local
				if root != "tvshow" && root != "episodedetails" {
					return ev, errors.New("invalid_nfo")
				}
			}
			if depth == 2 {
				field = t.Name.Local
				switch field {
				case "title", "showtitle", "season", "episode", "absolute_number":
					if _, ok := values[field]; ok {
						return ev, errors.New("invalid_nfo")
					}
					values[field] = ""
				default:
					field = ""
				}
			}
			if depth > 2 && field != "" {
				return ev, errors.New("invalid_nfo")
			}
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(t)) != "" {
				return ev, errors.New("invalid_nfo")
			}
			if depth == 2 && field != "" {
				values[field] += string(t)
			}
		case xml.EndElement:
			if depth == 2 {
				field = ""
			}
			depth--
			if depth == 0 {
				closed = true
			}
		}
	}
	if !closed || depth != 0 {
		return ev, errors.New("invalid_nfo")
	}
	name := strings.TrimSpace(values["showtitle"])
	if root == "tvshow" {
		ev.Kind = "series_nfo"
		name = strings.TrimSpace(values["title"])
	} else {
		ev.Kind = "episode_nfo"
		var err error
		ev.Season, err = strconv.Atoi(strings.TrimSpace(values["season"]))
		if err != nil || ev.Season < 1 || ev.Season > 99 {
			return ev, errors.New("invalid_nfo")
		}
		ev.Episode, err = strconv.Atoi(strings.TrimSpace(values["episode"]))
		if err != nil || ev.Episode < 1 || ev.Episode > 999 {
			return ev, errors.New("invalid_nfo")
		}
		if a := strings.TrimSpace(values["absolute_number"]); a != "" {
			ev.Absolute, err = strconv.Atoi(a)
			if err != nil || ev.Absolute < 1 || ev.Absolute > 999 {
				return ev, errors.New("invalid_nfo")
			}
		}
	}
	if len(name) > 256 || strings.ContainsAny(name, "/\\\x00\r\n") {
		return ev, errors.New("invalid_nfo")
	}
	if name != "" {
		if _, err := normalizeTitle(name); err != nil {
			return ev, errors.New("invalid_nfo")
		}
	}
	ev.DisplayName = name
	return ev, nil
}

// atime 会随只读访问变化，不属于候选身份；ctime/mtime/inode/size 均需稳定。
type libraryIdentity struct {
	Dev, Ino     uint64
	Mode         uint32
	Size         int64
	Mtime, Ctime unix.Timespec
}

func libraryStat(s unix.Stat_t) libraryIdentity {
	return libraryIdentity{uint64(s.Dev), s.Ino, s.Mode, s.Size, s.Mtim, s.Ctim}
}
