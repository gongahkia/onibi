# Files Panel Security

The file panel exposes session-local browse, view, and edit operations from the owner web cockpit. It is not a general file server.

## Auth and Scope

- `GET /files/tree`, `GET /files/content`, and `PUT /files/content` require owner HTTP auth.
- Requests name a session with `session` or `session_id`; the daemon resolves the session cwd from the active session list, then from SQLite if needed.
- The file root is the absolute session cwd. Empty, missing, or non-directory cwd values are rejected.
- Paths are always relative to the session cwd. Absolute paths, empty paths, `.` paths, and paths that resolve outside the cwd are rejected.
- Current handlers expose owner access only. A read-only viewer role must receive `403` for all file endpoints until a separate viewer-scoped API exists.

## File Tree

`GET /files/tree?session=<id>` returns a nested tree rooted at the session cwd.

Limits:

| guard | value |
|---|---:|
| max depth | `8` |
| max entries per directory | `200` |
| max JSON response | `1 MiB` |

The tree honors the root `.gitignore` and always ignores `.git/`. Truncated directories or responses are marked with `truncated`.

## File View

`GET /files/content?session=<id>&path=<rel>` returns file metadata and, for text files, content.

Read guards:

- Path normalization uses slash conversion, clean path resolution, and a relative-path check against the session root.
- Directories and symlink files are rejected.
- Files over `2 MiB` are rejected.
- Binary files return `mime`, `size`, and `binary: true`; file bytes are not returned.

## File Edit

`PUT /files/content?session=<id>&path=<rel>` accepts:

```json
{"content":"new file content"}
```

Write guards:

- Content over `2 MiB` is rejected before approval.
- Existing directories, symlink files, and binary files are rejected.
- The handler does not write immediately. It queues an approval request with tool `FileEdit` and returns `202 {"approval_id":"..."}`.
- The approval input contains `file_path` and `content`; the diff sent to the approval queue is scrubbed with the same redaction path used by tool approvals.
- `approve` writes the queued content. `edit` validates the edited `file_path` and `content` before writing. `deny`, `cancel`, expiry, or channel close writes nothing.
- Before writing, the daemon checks the parent path, rejects symlink parents, confirms the existing parent remains inside the session root, creates missing parent directories with `0700`, rejects a symlink destination, and writes file mode `0600`.

Diff approvals render inline in the phone card:

![Approval diff card](assets/approval-diff-card.svg)

## Residual Trust Boundary

Onibi still trusts the local OS user account. File-panel checks scope browser-origin requests to the session cwd and approval queue, but they are not a sandbox against the same local user changing files or paths while an approval is pending.
