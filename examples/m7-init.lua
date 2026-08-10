-- Experimental M7 trusted-local configuration example.
-- Run `zenbu-headless config-check examples/m7-init.lua` before using it.

zenbu.selector {
  id = "example.document",
  title = "Document",
  description = "Select the complete current document.",
  run = function(call)
    return {
      selections = {{ anchor = 0, head = call.context.document.length }},
      primary = 1,
    }
  end,
}

zenbu.transform {
  id = "example.bracket",
  title = "Bracket selection",
  description = "Insert brackets around every selected range.",
  run = function(call)
    local edits = {}
    for _, selection in ipairs(call.arguments.selection_set) do
      local start = math.min(selection.anchor, selection.head)
      local stop = math.max(selection.anchor, selection.head)
      table.insert(edits, { start = start, stop = start, text = "[" })
      table.insert(edits, { start = stop, stop = stop, text = "]" })
    end
    return { edits = edits }
  end,
}

zenbu.command {
  id = "example.wrap-document",
  title = "Wrap document",
  description = "Compose the example selector and transformation.",
  run = function(_)
    return {{
      kind = "apply",
      selector = "example.document",
      transformation = "example.bracket",
    }}
  end,
}

zenbu.bind {
  input = "Ctrl-K",
  command = "example.wrap-document",
  scope = "global",
}

zenbu.on {
  event = "document-changed",
  run = function(call)
    return {{ kind = "message", text = "observed " .. call.arguments.event }}
  end,
}
