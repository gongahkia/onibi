# Getting Started

## Python Environment

```bash
uv sync
```

## Runtime Dependencies

### macOS

```bash
brew install openjdk@11 joern
npm install --global typescript
```

### Validation

```bash
joern --version
java -version
npx tsc --version
```

## Phase 0 Acceptance Checks

```bash
uv sync
uv build
uv run piranesi --help
uv run piranesi scan . --authorized --yes
```
