# ADR 0012: display coordinates are host projections

## Context

Zenbu documents use UTF-8 byte offsets, while terminals render grapheme
clusters into display columns. Treating bytes as terminal cells breaks combining
marks, wide characters, tabs, selection highlighting, and cursor visibility.
Replacing kernel offsets with line/column coordinates would invalidate the
document and transaction invariants.

## Decision

Keep document, anchor, range, and selection coordinates as byte offsets.
`zenbu.view.Display` separately segments visible source lines with Uuseg
extended grapheme clusters and assigns display widths with Uucp's terminal
width hint. Tabs expand to four-column stops; C0/DEL controls are rendered as
safe caret notation. The viewport and frame use line/display-column positions;
the backend alone converts a frame cursor to a physical terminal cursor.

## Alternatives considered

- Store terminal cells in documents: terminal-specific, lossy, and incompatible
  with UTF-8 transaction boundaries.
- Use code-point columns: still splits user-visible graphemes and mishandles
  wide characters and tabs.
- Let Notty layout the whole document directly: couples view tests and source
  coordinates to the backend and offers no stable selection-to-cell mapping.

## Consequences

The renderer can be tested headlessly and layouts only viewport source lines.
Display width remains a terminal heuristic, especially for emoji and complex
scripts; M4 does not promise pixel-perfect terminal agreement.
