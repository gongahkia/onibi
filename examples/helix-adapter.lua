-- A deliberately small Helix-style view-navigation adapter for
-- `--model selection`.
--
-- It demonstrates that a selection editor can own its view-navigation grammar
-- through checked host requests while retaining no renderer, pane, or terminal
-- handle. It is an evaluation fixture, not a Helix compatibility layer.

zenbu.bind {
  input = "PageUp",
  command = "view.page.up",
  scope = "model:zenbu.selection-first:select",
}

zenbu.bind {
  input = "PageDown",
  command = "view.page.down",
  scope = "model:zenbu.selection-first:select",
}

zenbu.bind {
  input = "Ctrl-u",
  command = "view.page.up",
  scope = "model:zenbu.selection-first:select",
}

zenbu.bind {
  input = "Ctrl-d",
  command = "view.page.down",
  scope = "model:zenbu.selection-first:select",
}

zenbu.bind {
  input = "z z",
  command = "view.center",
  scope = "model:zenbu.selection-first:select",
}
