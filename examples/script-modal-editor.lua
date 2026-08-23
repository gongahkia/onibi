-- A deliberately small editor-family fixture. It is not a Vim replacement:
-- it demonstrates that one Lua declaration owns persistent grammar state,
-- exposes distinct status modes, and requests only checked Zenbu effects.

zenbu.model {
  id = "zenbu.example.modal",
  title = "Script modal fixture",
  description = "Persistent Lua-owned modal grammar for evaluation.",
  initial_state = { mode = "normal", count = 0 },
  initial_status = { id = "normal", label = "NORMAL", input_mode = "keys" },
  run = function(call)
    local input = call.arguments.input
    local state = call.arguments.state

    local function status(mode, pending)
      return {
        id = mode,
        label = string.upper(mode),
        input_mode = mode == "insert" and "text" or "keys",
        pending_input = pending,
      }
    end

    local function result(next, next_status, effects)
      return { state = next, status = next_status, effects = effects or {} }
    end

    if input.kind == "key" and input.key == "i" then
      return result({ mode = "insert", count = state.count }, status("insert"))
    elseif input.kind == "key" and input.key == "Escape" then
      return result({ mode = "normal", count = state.count }, status("normal"))
    elseif input.kind == "key" and input.key == "d" and state.mode == "normal" then
      return result({ mode = "delete", count = state.count }, status("delete", "d"))
    elseif input.kind == "key" and input.key == "w" and state.mode == "delete" then
      return result(
        { mode = "normal", count = state.count + 1 },
        status("normal"),
        {{ kind = "apply", selector = "current-word", transformation = "delete" }})
    elseif input.kind == "key" and input.key == "z" and state.mode == "normal" then
      return result(state, status("normal"), {{ kind = "view", action = "center" }})
    elseif input.kind == "key" and input.key == "s" and state.mode == "normal" then
      return result(
        state,
        status("normal"),
        {{ kind = "apply", selector = "current-word", transformation = "select" }})
    elseif input.kind == "key" and input.key == "u" and state.mode == "normal" then
      return result(
        state,
        status("normal"),
        {{
          kind = "external-filter",
          program = "/usr/bin/tr",
          arguments = { "a-z", "A-Z" },
        }})
    elseif input.kind == "key" and input.key == "j" and state.mode == "normal" then
      return result(
        state,
        status("normal"),
        {{
          kind = "background-process",
          program = "/usr/bin/printf",
          arguments = { "script background job" },
        }})
    elseif input.kind == "text" and state.mode == "insert" then
      return result(
        { mode = "insert", count = state.count + 1 },
        status("insert"),
        {{ kind = "insert", text = input.text }})
    else
      return result(state, status(state.mode))
    end
  end,
}
