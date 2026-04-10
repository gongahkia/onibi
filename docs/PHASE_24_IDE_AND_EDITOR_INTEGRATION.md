# Phase 24: IDE + Editor Integration

**Estimated effort: 40-50 ideal hours**
**Blocked by: Phase 14 (incremental scanning), Phase 17 (SARIF output)**
**Blocks: Nothing (independent feature)**

## 1. Motivation

CLI scanning during CI is essential but slow feedback loops hurt developer productivity. IDE integration via LSP (Language Server Protocol), watch mode, and pre-commit hooks bring security feedback to the development inner loop — where fixes are cheapest.

All integrations remain CLI-native: no IDE-specific plugins required. LSP is a standard protocol supported by VS Code, Neovim, Emacs, Helix, Sublime Text, and JetBrains IDEs.

## 2. Language Server Protocol (LSP) Adapter

### 2.1 Architecture

```
Editor (VS Code / Neovim / etc.)
  ↕ LSP JSON-RPC (stdio)
piranesi lsp (Python subprocess)
  ↕ incremental scan engine
Joern server (managed lifecycle)
```

The LSP server is a thin adapter: it translates LSP events into Piranesi scan invocations and Piranesi findings into LSP diagnostics.

### 2.2 Supported LSP Features

| LSP Method | Piranesi Action |
|------------|----------------|
| `textDocument/didOpen` | Register file for tracking |
| `textDocument/didSave` | Trigger incremental scan on file |
| `textDocument/didClose` | Unregister file |
| `textDocument/publishDiagnostics` | Push findings as diagnostics |
| `textDocument/codeAction` | Offer "Apply Piranesi fix" for patched findings |
| `textDocument/hover` | Show finding summary on taint source/sink |
| `textDocument/diagnostic` (pull model) | Return findings for requested file |

### 2.3 Diagnostic Mapping

```python
def finding_to_diagnostic(finding: CandidateFinding) -> Diagnostic:
    return Diagnostic(
        range=Range(
            start=Position(line=finding.source.location.line - 1, character=0),
            end=Position(line=finding.source.location.line - 1, character=999),
        ),
        severity=_severity_map[finding.severity],
        code=finding.vuln_class,
        code_description=CodeDescription(href=f"https://cwe.mitre.org/data/definitions/{finding.vuln_class.split('-')[1]}.html"),
        source="piranesi",
        message=f"{finding.vuln_class}: {finding.source.parameter_name} → {finding.sink.api_name}",
        related_information=[
            DiagnosticRelatedInformation(
                location=Location(uri=sink_uri, range=sink_range),
                message=f"Sink: {finding.sink.api_name} at line {finding.sink.location.line}",
            )
            for step in finding.taint_path  # intermediate steps as related info
        ],
    )
```

### 2.4 Code Actions

For findings with patches (`finding.patch is not None`):
```python
CodeAction(
    title=f"Apply Piranesi fix for {finding.vuln_class}",
    kind=CodeActionKind.QuickFix,
    diagnostics=[diagnostic],
    edit=WorkspaceEdit(changes={uri: [TextEdit(range, new_text)]}),
)
```

### 2.5 Debouncing

On `textDocument/didSave`, debounce scans:
- Default: 1000ms debounce.
- If a scan is already running, queue the next save and scan after completion.
- Max queue depth: 1 (latest save wins).

### 2.6 Joern Lifecycle

The LSP server manages Joern as a long-lived background process:
- Start Joern on first scan request.
- Keep alive between scans (avoid startup overhead).
- Shut down on LSP `shutdown` request.
- Auto-restart on Joern crash (once, then fail).

### 2.7 Configuration

```toml
[lsp]
enabled = true
scan_on_save = true
debounce_ms = 1000
max_findings_per_file = 50  # avoid flooding editor
severity_filter = "medium"  # only show medium+ in editor
```

### 2.8 CLI Command

```
piranesi lsp                          # start LSP server (stdio)
piranesi lsp --tcp --port 9257        # TCP mode for debugging
piranesi lsp --log /tmp/piranesi-lsp.log  # debug logging
```

### 2.9 Editor Configuration Examples

**Neovim (nvim-lspconfig):**
```lua
require('lspconfig').piranesi.setup {
    cmd = { 'piranesi', 'lsp' },
    filetypes = { 'typescript', 'javascript', 'python', 'go', 'java' },
    root_dir = require('lspconfig.util').root_pattern('piranesi.toml', 'package.json'),
}
```

**VS Code (settings.json):**
```json
{
    "piranesi.lsp.enable": true,
    "piranesi.lsp.scanOnSave": true,
    "piranesi.lsp.severityFilter": "medium"
}
```

### 2.10 Dependencies

Add to `[project.optional-dependencies]`:
```toml
lsp = ["pygls>=1.3.0"]
```

Install: `uv pip install piranesi[lsp]`

## 3. Watch Mode

### 3.1 File Watcher

`piranesi watch <dir>` starts a persistent file watcher that triggers incremental scans on changes.

```
$ piranesi watch ./src
Watching ./src for changes...
[14:23:01] Scanning 2 changed files...
[14:23:04] 3 findings (1 new, 2 unchanged)
[14:23:45] Scanning 1 changed file...
[14:23:46] 2 findings (0 new, 1 fixed, 1 unchanged)
^C
Summary: 4 scans, 3 findings remaining, 1 fixed
```

### 3.2 Terminal UI

Rich Live display showing:
- Current finding count (by severity)
- Last scan timestamp + duration
- Changed files since last scan
- Scan status (idle / scanning / error)

### 3.3 CLI Flags

```
piranesi watch <dir>
    --filter "**/*.ts"           # only watch matching files
    --debounce 500               # debounce ms (default 500)
    --on-finding "notify-send 'Piranesi: {count} findings'"  # hook
    --fail-severity high         # exit 1 on high+ findings (for CI watch mode)
    --max-scans 10               # exit after N scans (for testing)
```

### 3.4 Hook System

`--on-finding <cmd>` executes a shell command when new findings appear. Template variables:
- `{count}` — total finding count
- `{new}` — new findings since last scan
- `{fixed}` — fixed findings since last scan
- `{severity}` — highest severity found

### 3.5 Dependencies

Add to `[project.optional-dependencies]`:
```toml
watch = ["watchfiles>=0.21.0"]
```

## 4. Git Pre-Commit Hook

### 4.1 Hook Installation

```
piranesi hook install                  # install pre-commit hook
piranesi hook uninstall                # remove hook
piranesi hook status                   # show hook status
```

Writes to `.git/hooks/pre-commit`:
```bash
#!/bin/sh
# piranesi pre-commit hook
exec piranesi run --incremental --staged-only --fail-severity high --timeout 60 "$@"
```

### 4.2 Staged-Only Scanning

`--staged-only` flag:
1. Run `git diff --cached --name-only --diff-filter=ACMR` to get staged files.
2. Filter to supported file types.
3. Pass as `changed_files` to incremental scan.
4. Only report findings in staged files (ignore rest of codebase).

### 4.3 Timeout

`--timeout <seconds>` (default 60): if scan exceeds timeout, print warning and exit 0 (don't block commit). Developers can always run a full scan separately.

### 4.4 pre-commit Framework Integration

Generate `.pre-commit-hooks.yaml` for use with the `pre-commit` tool:

```yaml
- id: piranesi
  name: Piranesi Security Scan
  entry: piranesi run --incremental --staged-only --fail-severity high
  language: python
  types_or: [typescript, javascript, python, go, java]
  require_serial: true
```

### 4.5 Configuration

```toml
[hooks]
pre_commit = true
fail_severity = "high"    # only block commit on high+
timeout = 60              # seconds
staged_only = true        # only scan staged files
```

## 5. Tests

### LSP Tests
1. Mock LSP client sends `textDocument/didSave` → verify diagnostics published.
2. Verify diagnostic severity mapping (high → Error, medium → Warning, etc.).
3. Verify code action offered for findings with patches.
4. Verify debouncing: 3 rapid saves → only 1 scan.
5. Verify Joern lifecycle: start on first scan, keep alive, shutdown on exit.

### Watch Mode Tests
1. Mock file change events → verify incremental scan triggered.
2. Verify debouncing: rapid changes → single scan.
3. Verify `--on-finding` hook executes with correct template variables.
4. Verify `--max-scans` exits after N scans.

### Pre-Commit Hook Tests
1. Mock `git diff --cached` → verify only staged files scanned.
2. Verify timeout: slow scan exits 0 with warning.
3. Verify `--fail-severity high`: medium findings exit 0, high findings exit 1.
4. Verify hook install/uninstall writes/removes `.git/hooks/pre-commit`.

## 6. Risks

- **LSP performance**: Joern startup is slow (~5s). Mitigation: keep Joern alive between scans.
- **Watch mode memory**: long-running process may leak. Mitigation: periodic Joern restart (every 100 scans).
- **Pre-commit UX**: slow hooks frustrate developers. Mitigation: 60s timeout, staged-only, skip on `--no-verify`.
- **Editor compatibility**: LSP implementations vary. Mitigation: test with Neovim + VS Code (covers 90% of users).
