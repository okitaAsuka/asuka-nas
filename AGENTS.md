# asuka-nas collaboration

Read docs/DESIGN.md and docs/STATUS.md before changes. This repository owns NAS state and file execution; do not modify asuka-agent. No real RSS, qBittorrent, Emby or NAS access during development. Fixtures and fake HTTP only. No credentials in commits/logs, no token creation or permission changes. Keep source files; no delete or copy fallback. Do not merge or deploy without explicit authorization. Core boundaries need concise Chinese comments. Run `make verify` using real Go 1.26.9. Update status and contracts with behavior changes.
