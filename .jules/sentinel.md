## 2026-10-31 - Receiver MAC Parameter Path Traversal Prevention
**Vulnerability:** HTTP query parameter `mac` was passed directly to bookmark lookup routines in `internal/server/server.go`, potentially allowing path traversal sequences (such as `../`) to be processed.
**Learning:** While the `favorite` endpoint had regex validation on the `mac` parameter, other endpoints (`bookmarkList`, `station`, and `login`) lacked equivalent validation at the HTTP boundary.
**Prevention:** Validate all receiver identifiers against `safeBookmarkMAC` (`^[A-Za-z0-9_-]{1,64}$`) at the request boundary before handing them to internal storage layers.
