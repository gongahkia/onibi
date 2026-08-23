-- A deliberately small Micro-style adapter for `--model direct`.
--
-- It demonstrates that trusted adapters can redirect non-reserved input to
-- bounded host workspace operations without receiving document or filesystem
-- authority. It is not a Micro parity configuration.

zenbu.bind {
  input = "Ctrl-e",
  command = "editor.command-palette",
  scope = "model:zenbu.direct:direct",
}

zenbu.bind {
  input = "Ctrl-w",
  command = "workspace.pane.next",
  scope = "model:zenbu.direct:direct",
}

zenbu.bind {
  input = "Ctrl-x",
  command = "editor.kill-ring.cut",
  scope = "model:zenbu.direct:direct",
}

zenbu.bind {
  input = "Ctrl-c",
  command = "editor.clipboard.copy",
  scope = "model:zenbu.direct:direct",
}

zenbu.bind {
  input = "Ctrl-v",
  command = "editor.clipboard.paste",
  scope = "model:zenbu.direct:direct",
}
