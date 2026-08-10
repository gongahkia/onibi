zenbu.selector {
  id = "zenbu.example-surround.document",
  title = "Example document",
  description = "Select the full current document.",
  run = function(call)
    return {
      selections = {{ anchor = 0, head = call.context.document.length }},
      primary = 1,
    }
  end,
}

zenbu.transform {
  id = "zenbu.example-surround.bracket",
  title = "Example brackets",
  description = "Wrap the selected ranges in square brackets.",
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
  id = "zenbu.example-surround.wrap",
  title = "Example wrap",
  description = "Compose the plugin selector and transformation.",
  run = function(_)
    return {{
      kind = "apply",
      selector = "zenbu.example-surround.document",
      transformation = "zenbu.example-surround.bracket",
    }}
  end,
}

zenbu.bind { input = "Ctrl-K", command = "zenbu.example-surround.wrap", scope = "global" }

zenbu.on {
  event = "document-changed",
  run = function(_) return {{ kind = "message", text = "example surround changed the document" }} end,
}
