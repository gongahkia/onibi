# mermaidascii

Render [Mermaid](https://mermaid.js.org) diagrams as ASCII or Unicode box
drawings — in pure Go, with nothing outside the standard library.

```go
out, _ := mermaidascii.Render(`flowchart TD
    A[Start] --> B{Ready?}
    B -->|yes| C[Go]
    B -->|no| D[Wait]`)
fmt.Print(out)
```

```
┌────────┐
│        │
│ Start  │
│        │
└────┬───┘
     │
     │
     │
     │
     ▼
◇────────◇
│        │        no
│ Ready? ├─────────┐
│        │         │
◇────┬───◇         │
     │             │
     │             │
     │             │
     │             │
 yes ▼             ▼
┌────────┐     ┌──────┐
│        │     │      │
│   Go   │     │ Wait │
│        │     │      │
└────────┘     └──────┘
```

## Why

There is already a Go tool that does this, [mermaid-ascii][ma], and a
TypeScript library, [beautiful-mermaid][bm]. This is neither, and owes
everything to both — see [NOTICE](NOTICE).

It exists because mermaid-ascii is a command, not a library: its core sits in
`package cmd` next to cobra, logrus and a gin web server, so importing its one
exported entry point takes a Go binary from 1.1 MB to 12 MB. beautiful-mermaid
draws the better picture — it gets node shapes right, where mermaid-ascii turns
`B{Decision}` into two nodes — but calling it from Go means shipping a
JavaScript runtime.

So: the drawing of beautiful-mermaid, in Go, importable, dependency-free.

[ma]: https://github.com/AlexanderGrooff/mermaid-ascii
[bm]: https://github.com/lukilabs/beautiful-mermaid

## Install

```sh
go get github.com/coreequip/mermaidascii
go install github.com/coreequip/mermaidascii/cmd/mermaidascii@latest
```

## Command line

```sh
mermaidascii diagram.mmd
cat diagram.mmd | mermaidascii
mermaidascii -ascii -x 3 diagram.mmd
```

| Flag | Meaning |
| :--- | :--- |
| `-ascii` | draw with `+ - |` instead of box-drawing characters |
| `-x`, `-y` | space between nodes, in characters (default 5) |
| `-padding` | space between a label and its border (default 1) |

## Library

```go
out, err := mermaidascii.Render(src,
    mermaidascii.WithASCII(),
    mermaidascii.WithSpacing(3, 2),
)
```

`Render` is deterministic: the same source always yields the same drawing,
which makes the output safe to commit and to compare in tests.

## What it understands

Flowcharts — `flowchart` or `graph`, direction `LR`, `RL`, `TD`, `TB`, `BT`:

- node shapes: `[rectangle]`, `(rounded)`, `([stadium])`, `[(cylinder)]`,
  `((circle))`, `{diamond}`, `{{hexagon}}`, `[[subroutine]]`, `[/parallelogram/]`
- edges: `-->`, `---`, `-.->`, `==>`, `<-->`, with labels in both spellings
  (`-->|text|` and `-- text -->`)
- chaining (`A --> B --> C`) and parallel links (`A & B --> C & D`)
- `<br>` in labels becomes a line break

Sequence diagrams — `sequenceDiagram`:

- `participant A as Alice` and `actor B`
- arrows `->`, `-->`, `->>`, `-->>`, `-)`, `--)`, `-x`, `--x`
- activation with `->>+` and `-->>-`
- blocks `loop`, `alt` / `else`, `opt`, `par` / `and`, `critical`, `break`, `rect`
- `Note left of A:`, `Note right of A:`, `Note over A,B:`
- messages an actor sends to itself, drawn as a loop beside its lifeline

Not yet: subgraphs, class and ER diagrams, styling. See
[docs/DEVIATIONS.md](docs/DEVIATIONS.md) for the current gaps and how output
differs from the reference implementation.

## Testing

```sh
go test ./...
```

The suite renders `testdata/input/*.mmd` and compares against
`testdata/golden/`, which holds the output of beautiful-mermaid, byte for
byte. Fixtures that are not reproduced yet are skipped with a stated reason
rather than deleted, so the suite reports what is missing.

`tools/gen-fixtures.sh` regenerates the golden files from the pinned upstream
commit. It needs bun and network access.

## Licence

MIT. The lineage and its copyright holders are recorded in [NOTICE](NOTICE).
