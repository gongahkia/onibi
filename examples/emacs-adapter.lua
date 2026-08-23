-- A deliberately small Emacs-style adapter for `--model direct`.
--
-- The Direct model owns C-w for cutting non-empty selections and the C-x
-- prefix. This adapter redirects C-y to Zenbu's newest shared kill entry.
-- It is an evaluation fixture, not an Emacs compatibility layer.

zenbu.bind {
  input = "Ctrl-y",
  command = "editor.kill-ring.yank",
  scope = "model:zenbu.direct:direct",
}
