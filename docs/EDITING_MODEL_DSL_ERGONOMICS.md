# Editing-model DSL ergonomics experiment

This document records an experiment against DSL v1. It is evidence for future
work, not a promise that `.zenmodel` replaces Zenbu's first-party models.

## Vocabulary available in v1

DSL `apply` resolves only the built-in `Model_intent` vocabulary.

Selectors:

- `current-selections`, `document`
- `next-text-unit`, `previous-text-unit`
- `next-word`, `previous-word`, `word-end`, `current-word`, `around-word`
- `current-line`, `line-start`, `line-end`, `first-nonblank`
- `document-start`, `document-end`, `next-line`, `previous-line`
- `all-occurrences`

Transformations:

- `select`
- `delete`
- `collapse-to-start`, `collapse-to-end`
- `replace:TEXT`, including a literal escaped newline such as `"replace:\n"`

The experiment uses all transformation families. It does not assume that a
convenient first-party operation is automatically a DSL operation.

## Models tested

| Model | File | Declared states | Transitions | Generated prefix nodes | What it demonstrates |
| --- | --- | ---: | ---: | ---: | --- |
| Modal operator | `examples/modal-operator.zenmodel` | 2 | 20 | 3 (`d`, `c`, `v`) | Modal text entry, operator-like multi-key rules, navigation, selection, deletion, committed text |
| Selection first | `examples/selection-first.zenmodel` | 2 | 19 | 1 (`g`) | Selection-producing keys followed by selection transformations |
| Direct | `examples/direct.zenmodel` | 1 | 19 | 0 | Non-modal text entry, named-key navigation, shift selection, immediate edits |

The counts are asserted by `test_model_dsl_examples`. Prefix counts are the
immutable trie nodes reported by `Compile.prefixes`, not extra user states.

### Modal operator

The modal grammar is readable at this size. `d w`, `d a`, and `d l` share an
inspectable prefix without an explicit `DELETE…` state. `c w` and `c l` prove
that a completed sequence can enter text mode after returning an existing
semantic effect. `v w` and `v l` show that an operator-looking syntax is not
special: it is an ordinary selector plus `select`.

It is deliberately much smaller than `models/vim_model.ml`. It cannot express
counts, registers, yanks/pastes, macro/location prefixes, find-target input,
repeat/search effects, or the first-party model's context-derived line-change
operation.

### Selection first

This grammar is not merely a modal fixture with different labels: `w` creates
a persistent ordinary Zenbu selection, and subsequent `d` or `c` acts through
`current-selections`. The test checks the resulting document transition
`"alpha beta" -> "beta"` after `w`, then `d`.

It covers the built-in selection vocabulary naturally. It cannot reach the
registered selection-algebra commands used by `models/selection_model.ml`:
regex selection/splitting/filtering, `editor.selection.merge-consecutive`,
`editor.selection.rotate-primary-forward`,
`editor.selection.rotate-primary-backward`, content rotation,
`editor.selection.flip`, and `editor.selection.ensure-forward`.

### Direct

The direct grammar has one `text` state: committed text inserts immediately;
arrows and `Ctrl-b/f/p/n` use ordinary selector transformations; Shift-arrows
produce a visible selection; Backspace/Delete are immediate semantic edits.
`Enter` demonstrates existing literal replacement through `replace:\n`.

It does not reproduce `models/direct_model.ml` exactly. That model deletes the
current non-empty selection when one exists, otherwise an adjacent text unit,
and also exposes save, history, clipboard, and workspace effects. The DSL has
neither a read-only condition nor those effect forms.

## What worked naturally

- Key spelling, modifiers, named keys, and committed UTF-8 text were reused
  without a second input grammar.
- Multi-key operator syntax compiled to small deterministic tries. No timeout,
  precedence rule, or custom pending-state code was needed.
- Built-in selection and transformation composition spans modal,
  selection-first, and direct interaction styles.
- Text entry remains a single singleton `<text>` transition per state.
- Reusing an existing selector with a different transformation was concise and
  kept semantic replay model-neutral.

## Repetition and state pressure

The three models do not show unreasonable finite-state pressure: only five
user-declared states total represent three distinct styles. Prefixes add four
generated nodes, not source states.

There are seven repeated effect-body groups and one repeated control rule:

- Modal: `current-word/delete` and `current-line/delete` each occur in delete
  and change rules; the no-effect Escape-to-normal control transition occurs
  twice.
- Selection first: `current-selections/delete` occurs in both `d` and `c`.
- Direct: each of previous/next text-unit and previous/next line navigation is
  duplicated between an arrow and a control binding.

This is noticeable but not yet severe. It supports small compile-time action
reuse; it does not yet justify transition fragments. Only the modal grammar
repeats an identical Escape rule, and only twice.

## Friction log

| Class | Issue and affected models | Current form | Why it matters | Smallest plausible improvement | Keep in Lua/OCaml? |
| --- | --- | --- | --- | --- | --- |
| A | Repeated effect bodies in all three models | Repeat an `apply` body at each binding | 7 duplicate effect-body groups across 58 transitions; target state may differ | Compile-time `action` definitions reused from transition bodies | No; finite expansion is appropriate |
| A | Repeated Escape/cancel transitions | Repeat rules in each relevant state | Only two identical rules in this sample | Do not add fragments yet; revisit with larger multi-state grammars | N/A |
| B | Selection algebra is unreachable from selection-first DSL | Built-in selectors cover navigation only | Existing commands provide merge, primary rotation, flip, ensure-forward, regex operations, and content rotation | Registered command invocation, validated against ordinary descriptors | No for descriptor-backed operations |
| B | Host/history/clipboard/workspace effects are unreachable from direct DSL | Bind only `apply` or captured insert | Direct-model parity is intentionally incomplete | Do not add DSL-native forms one-by-one; assess existing command boundary first | Usually host/OCaml model territory |
| B | Syntax-node navigation is unreachable | Structural model calls `Syntax.Selector` and builds selections | DSL has no syntax selector IDs and no structural effect | Do not expose a special case; command/semantic-operation design needs separate evidence | Often OCaml/structural model territory |
| C | Direct Delete/Backspace needs selection-dependent behavior | Always delete next/previous text unit | Diverges when a non-empty selection already exists | Small built-in guards with `selection.any_nonempty` and an `else` branch | No; this is a pure model-neutral decision |
| C | Structural commands require syntax availability | First-party model branches on `Editor_context.syntax` | A future structural grammar cannot safely select a syntax node otherwise | `syntax.available` only if structural DSL is later in scope | No, but defer until a structural DSL experiment |
| D | Counts in Vim and selection-first models | First-party models accumulate digits and repeat operations | Requires mutable numeric state and arithmetic | None for v1.1 | Yes |
| D | Registers, macro/location names, find targets, and structural shrink stack | First-party model-local data and algorithms | Finite expansion would be brittle and large | None for v1.1 | Yes |

`selection.primary.empty`, `selection.any_nonempty`, `selection.multiple`, and
`syntax.available` are the exact candidate predicates observed here. Only
`selection.any_nonempty` has immediate evidence from the direct model.

## Inspection and describe output

Source inspection of `Describe.render` shows that it preserves declaration
order, prints every transition with its source location and direct effect, then
prints generated prefixes. At 20 transitions that structure is still tractable:
the modal model exposes `d`, `c`, and `v`; selection-first exposes `g`; direct
has no prefix noise. A runnable development environment must still execute the
focused tests and `model-describe` commands before treating that observation as
an empirical terminal-output result. The focused tests assert deterministic
`Describe.render` output and the prefix counts.

Generic `Input_rule` inspection is adequate for immediate bindings, the
currently available prefix continuations, state label, and `<text>` catch-all.
It cannot show a whole multi-event sequence or DSL source span in the generic
binding list. That is useful future inspection work, not an `Input_rule`
correctness defect and not a reason to expand it in v1.1.

## Ranked v1.1 recommendation

| Rank | Feature | Evidence | Complexity / risk | Recommendation |
| ---: | --- | --- | --- | --- |
| 1 | Compile-time action reuse | Every model has repeated effect bodies; seven groups total | Low; expands to existing typed IR and adds no runtime authority | Yes |
| 2 | Tiny built-in guards with ordered `else` | Direct delete/backspace needs `selection.any_nonempty`; future structural use would need `syntax.available` | Medium; predicates must be synchronous, deterministic, and limited to `Editor_context` | Yes, starting only with observed predicates |
| 3 | Registered command invocation without arguments | Selection-first cannot access `editor.selection.merge-consecutive`, both primary rotations, `editor.selection.flip`, or `editor.selection.ensure-forward` | Medium; activation must validate against the normal command registry before input handling | Yes |
| 4 | Typed command arguments | Regex and grouped content rotation would eventually need named `Text` arguments | Medium/high; source syntax and descriptor validation add surface area | Defer until a concrete command model proves no-argument invocation insufficient |
| 5 | Transition fragments | Only two identical Escape rules occur in these models | Low/medium, but weak evidence | Defer |

The recommended command boundary is existing registered commands only. It
must not synthesize input or grant filesystem, process, terminal, renderer,
Lua, or Wasm access. A later argument design should use descriptor-declared
named kinds (`Text`, `Selector`, `Transformation`), not expressions; v1.1
should begin with no arguments.

Do not add variables, counts, registers, loops, callbacks, general expressions,
syntax-specific primitives, or special-purpose host effects in response to this
experiment. Those either require fundamentally programmable state or need a
separate semantic-boundary proposal.

## Conclusion

DSL v1 is credible as Zenbu's primary declarative surface for finite,
inspectable editing grammars built from existing selector/transformation
semantics. It is not a replacement for the first-party Vim, direct,
selection-first, or structural models. The evidence supports a small v1.1
focused on reusable actions, pure built-in guards, and carefully validated
existing command reach—not a second general-purpose scripting language.

## Resulting version-1 additions

The evidence-backed additions were implemented without changing the language
header: these are optional constructs and existing `zenbu-model 1` files keep
their semantics. This is a follow-up to the experiment, not a rewrite of its
historical observations.

| Model | States | Compiled input transitions | Source arms | Prefix nodes | Actions / `do` uses | Guard groups | Commands |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Modal operator | 2 | 20 | 20 | 3 | 2 / 4 | 0 | 0 |
| Selection first | 2 | 24 | 24 | 1 | 0 / 0 | 0 | 5 |
| Direct | 1 | 19 | 21 | 0 | 0 / 0 | 2 | 0 |

The modal grammar removes four repeated inline delete bodies using two
compile-time actions. Selection-first now reaches five existing model-neutral
selection commands: merge consecutive selections, rotate the primary forward
or backward, flip orientation, and ensure forward orientation. Direct now
expresses the previously impossible delete/backspace choice with two guarded
groups; its source has 21 arms, but its trie still has the same 19 complete
input transitions because each guarded pair is one input binding.

This resolves the observed A, B, and C issues without responding to D with
variables or a general expression language. Fragments remain unsupported: the
two repeated Escape bindings alone remain insufficient evidence. Typed command
arguments, counts, registers, macros, dynamic targets, structural stacks,
loops, callbacks, and arbitrary mutable model state remain OCaml/Lua work.
