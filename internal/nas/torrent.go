package nas

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// torrentNode 保留原始字节范围，infohash 绝不依赖重新编码。
type torrentNode struct {
	kind       byte
	start, end int
	value      []byte
	number     int64
	list       []torrentNode
	dict       map[string]torrentNode
}
type torrentParser struct {
	b        []byte
	i, nodes int
}

var errTorrent = errors.New("torrent_invalid")

func (p *torrentParser) node(depth int) (torrentNode, error) {
	n := torrentNode{start: p.i}
	p.nodes++
	if depth > 32 || p.nodes > 50000 || p.i >= len(p.b) {
		return n, errTorrent
	}
	n.kind = p.b[p.i]
	switch n.kind {
	case 'i':
		p.i++
		start := p.i
		for p.i < len(p.b) && p.b[p.i] != 'e' {
			p.i++
		}
		if p.i == len(p.b) {
			return n, errTorrent
		}
		s := string(p.b[start:p.i])
		if s == "" || s == "-0" || strings.HasPrefix(s, "+") || len(s) > 1 && s[0] == '0' || strings.HasPrefix(s, "-0") {
			return n, errTorrent
		}
		v, e := strconv.ParseInt(s, 10, 64)
		if e != nil {
			return n, errTorrent
		}
		n.number = v
		p.i++
	case 'l', 'd':
		p.i++
		var prev []byte
		if n.kind == 'd' {
			n.dict = map[string]torrentNode{}
		}
		for p.i < len(p.b) && p.b[p.i] != 'e' {
			if n.kind == 'd' {
				key, e := p.node(depth + 1)
				if e != nil || key.kind != 's' || prev != nil && bytes.Compare(prev, key.value) >= 0 {
					return n, errTorrent
				}
				prev = key.value
				v, e := p.node(depth + 1)
				if e != nil {
					return n, e
				}
				n.dict[string(key.value)] = v
			} else {
				v, e := p.node(depth + 1)
				if e != nil {
					return n, e
				}
				n.list = append(n.list, v)
			}
		}
		if p.i >= len(p.b) {
			return n, errTorrent
		}
		p.i++
	default:
		n.kind = 's'
		start := p.i
		for p.i < len(p.b) && p.b[p.i] >= '0' && p.b[p.i] <= '9' {
			p.i++
		}
		if p.i == start || p.i >= len(p.b) || p.b[p.i] != ':' || p.i-start > 1 && p.b[start] == '0' {
			return n, errTorrent
		}
		size, e := strconv.ParseUint(string(p.b[start:p.i]), 10, 32)
		p.i++
		if e != nil || size > uint64(len(p.b)-p.i) {
			return n, errTorrent
		}
		n.value = p.b[p.i : p.i+int(size)]
		p.i += int(size)
	}
	n.end = p.i
	return n, nil
}

func torrentComponent(n torrentNode) bool {
	s := string(n.value)
	return n.kind == 's' && len(s) > 0 && len(s) <= 255 && utf8.ValidString(s) && s != "." && s != ".." && !strings.ContainsAny(s, "/\\") && strings.IndexFunc(s, unicode.IsControl) < 0
}

// TorrentHash 校验 BEP3 元数据、文件路径和分片数量；仅支持 v1，不执行任何下载。
func TorrentHash(b []byte) (string, error) {
	if len(b) == 0 || len(b) > 8*1024*1024 {
		return "", errTorrent
	}
	p := torrentParser{b: b}
	root, e := p.node(0)
	if e != nil || p.i != len(b) || root.kind != 'd' {
		return "", errTorrent
	}
	info := root.dict["info"]
	if info.kind != 'd' || !torrentComponent(info.dict["name"]) {
		return "", errTorrent
	}
	if alt, ok := info.dict["name.utf-8"]; ok && (!torrentComponent(alt) || !bytes.Equal(alt.value, info.dict["name"].value)) {
		return "", errTorrent
	}
	if _, ok := info.dict["symlink path"]; ok {
		return "", errTorrent
	}
	if _, ok := info.dict["meta version"]; ok {
		return "", errors.New("torrent_v2_unsupported")
	}
	if bytes.Contains(info.dict["attr"].value, []byte("l")) {
		return "", errTorrent
	}
	piece := info.dict["piece length"]
	pieces := info.dict["pieces"]
	if piece.kind != 'i' || piece.number <= 0 || pieces.kind != 's' || len(pieces.value)%20 != 0 {
		return "", errTorrent
	}
	length, single := info.dict["length"]
	files, multiple := info.dict["files"]
	if single == multiple {
		return "", errTorrent
	}
	total := int64(0)
	if single {
		if length.kind != 'i' || length.number < 0 {
			return "", errTorrent
		}
		total = length.number
	} else {
		if files.kind != 'l' || len(files.list) == 0 || len(files.list) > 4096 {
			return "", errTorrent
		}
		seen := map[string]bool{}
		for _, file := range files.list {
			size := file.dict["length"]
			parts := file.dict["path"]
			if file.kind != 'd' || size.kind != 'i' || size.number < 0 || size.number > math.MaxInt64-total || parts.kind != 'l' || len(parts.list) == 0 || bytes.Contains(file.dict["attr"].value, []byte("l")) {
				return "", errTorrent
			}
			var names []string
			for _, part := range parts.list {
				if !torrentComponent(part) {
					return "", errTorrent
				}
				names = append(names, string(part.value))
			}
			if _, ok := file.dict["symlink path"]; ok {
				return "", errTorrent
			}
			if alt, ok := file.dict["path.utf-8"]; ok {
				if alt.kind != 'l' || len(alt.list) != len(parts.list) {
					return "", errTorrent
				}
				for i, part := range alt.list {
					if !torrentComponent(part) || !bytes.Equal(part.value, parts.list[i].value) {
						return "", errTorrent
					}
				}
			}
			name := strings.Join(names, "/")
			if seen[name] || len(name) > 4095 {
				return "", errTorrent
			}
			seen[name] = true
			total += size.number
		}
	}
	count := int64(0)
	if total > 0 {
		count = (total-1)/piece.number + 1
	}
	if count != int64(len(pieces.value)/20) {
		return "", errTorrent
	}
	// Web seeds can trigger arbitrary HTTP access inside the downloader; exclude from this slice.
	if _, ok := root.dict["url-list"]; ok {
		return "", errors.New("torrent_webseed_unsupported")
	}
	if _, ok := root.dict["httpseeds"]; ok {
		return "", errors.New("torrent_webseed_unsupported")
	}
	sum := sha1.Sum(b[info.start:info.end])
	return hex.EncodeToString(sum[:]), nil
}
