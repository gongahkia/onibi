# Update Check JSON Schema

`onibi update-check --json` emits a versioned JSON object for release, package, and monitoring automation.

The current schema version is `"1"`.

## Example

```json
{
  "schema_version": "1",
  "status": "outdated",
  "source": "github",
  "current_version": "v0.3.0",
  "current_commit": "abc1234",
  "latest_version": "v0.4.0",
  "install_source": "homebrew-cask",
  "package_state": "homebrew-outdated",
  "url": "https://github.com/gongahkia/onibi/releases/tag/v0.4.0",
  "etag": "\"abc\"",
  "last_modified": "Mon, 01 Jan 2026 00:00:00 GMT",
  "detail": "latest release v0.4.0 is newer than v0.3.0",
  "command": "brew update && brew upgrade --cask onibi && onibi doctor --after-upgrade --offline"
}
```

## Fields

| field | type | required | example | meaning |
|---|---|---:|---|---|
| `schema_version` | string | yes | `"1"` | Output contract version. |
| `status` | string | yes | `"current"` | One of `current`, `outdated`, or `unavailable`. |
| `source` | string | yes | `"github"` | Metadata source: `local`, `github`, or `none`. |
| `current_version` | string | yes | `"v0.3.0"` | Version embedded in the running binary. |
| `current_commit` | string | yes | `"abc1234"` | Commit embedded in the running binary, or `unknown`. |
| `latest_version` | string | no | `"v0.4.0"` | Latest release tag when GitHub metadata is available. |
| `latest_commit` | string | no | `"abc123def456"` | Local source checkout commit when `source` is `local`. |
| `repo_dir` | string | no | `"/Users/me/src/onibi"` | Local Onibi checkout used for source-build checks. |
| `install_source` | string | no | `"homebrew-cask"` | Detected install source: `source`, `homebrew-cask`, or `release-archive`. |
| `package_state` | string | no | `"homebrew-outdated"` | Package-manager state when available. |
| `url` | string | no | `"https://github.com/gongahkia/onibi/releases/tag/v0.4.0"` | Release metadata URL. |
| `etag` | string | no | `"\"abc\""` | HTTP ETag cached for conditional GitHub checks. |
| `last_modified` | string | no | `"Mon, 01 Jan 2026 00:00:00 GMT"` | HTTP Last-Modified value cached for conditional GitHub checks. |
| `detail` | string | yes | `"current v0.4.0 is up to date with latest release v0.4.0"` | Human-readable status detail. |
| `command` | string | no | `"onibi update && onibi doctor --after-upgrade --offline"` | Suggested next command when an update or verification step is available. |

## Compatibility

Consumers must branch on `schema_version` before parsing fields. For schema version `"1"`, new fields may be added without a schema bump. Existing fields keep their type and meaning. Removing a field, renaming a field, changing a field type, or changing enum semantics requires a new schema version.

Treat unknown fields as optional metadata. Treat missing optional fields as unavailable data, not as a failed update check.

## Verification

```bash
onibi update-check --json | jq -e '.schema_version == "1" and (.status | type == "string") and (.detail | type == "string")'
```
