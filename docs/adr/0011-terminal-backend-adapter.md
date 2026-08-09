# ADR 0011: terminal backend behind a narrow adapter

## Context

M4 needs decoded UTF-8 input, raw-mode lifecycle handling, an alternate screen,
cursor placement, resize events, and styled cell output. Those requirements are
host concerns; exposing a terminal package in the kernel or model API would make
one backend a semantic dependency.

## Decision

Use `notty-community` 0.2.4 through `zenbu.terminal.Backend`. The adapter is
the only module that imports Notty or `Notty_unix`. It creates the terminal in
exclusive fullscreen mode with mouse and bracketed paste disabled, maps Notty
events to Zenbu's own `Terminal.Event`, and draws only pure `zenbu.view.Frame`
values. `Backend.t` is opaque and no public terminal type mentions Notty.

`Backend.with_terminal` releases the terminal in `Fun.protect`; the underlying
terminal is also created with its process-exit disposal fallback. Release is
idempotent in the backend package and restores input mode, cursor, and normal
screen.

## Alternatives considered

- Original `notty`: provides the same style of interface but is not the chosen
  maintained package line for this project.
- `lambda-term`: supports richer widgets and asynchronous application patterns,
  but brings a much larger widget/event abstraction than the M4 host needs.
- Direct ANSI escape output plus a custom parser: would duplicate raw-mode,
  UTF-8 input, resize, and cleanup work before there is evidence Zenbu needs a
  backend-specific implementation.

## Consequences

The interactive executable requires stdin and stdout TTYs and currently has one
fullscreen backend. Terminal package types cannot reach models or the kernel.
The adapter can be replaced later without changing `Input_event`, models,
sessions, or the pure renderer. Mouse, paste, and terminal-specific advanced
capabilities remain intentionally unavailable.
