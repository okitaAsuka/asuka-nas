package nas

import (
	"path"
	"strings"
)

// singleVideo 对整个下载器清单裁决；不允许只挑首个视频或忽略不明字幕。
func singleVideo(sources []string) (string, string) {
	var videos []string
	seen := map[string]bool{}
	for _, src := range sources {
		if !local(src) || seen[src] {
			return "", "invalid_file_manifest"
		}
		seen[src] = true
		switch strings.ToLower(path.Ext(src)) {
		case ".mkv", ".mp4", ".avi", ".mov", ".m4v", ".ts", ".m2ts", ".webm", ".mpg", ".mpeg", ".wmv", ".flv", ".vob", ".iso", ".ogm":
			videos = append(videos, src)
		case ".srt", ".ass", ".ssa", ".vtt", ".sub", ".idx", ".sup":
		case ".nfo", ".txt", ".jpg", ".jpeg", ".png", ".sfv", ".md5", ".sha1", ".sha256", ".ttf", ".otf":
		default:
			return "", "unsupported_file_set"
		}
	}
	if len(videos) != 1 {
		return "", "multiple_or_missing_videos"
	}
	video := videos[0]
	ext := strings.ToLower(path.Ext(video))
	if ext != ".mkv" && ext != ".mp4" {
		return "", "unsupported_video"
	}
	stem := strings.TrimSuffix(path.Base(video), path.Ext(video))
	for _, src := range sources {
		switch strings.ToLower(path.Ext(src)) {
		case ".srt", ".ass", ".ssa", ".vtt", ".sub", ".idx", ".sup":
			name := path.Base(src)
			if path.Dir(src) != path.Dir(video) || !strings.HasPrefix(name, stem) || !subtitleSuffix.MatchString(strings.TrimPrefix(name, stem)) {
				return "", "subtitle_mapping_ambiguous"
			}
		}
	}
	return video, ""
}
