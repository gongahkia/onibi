def run_compiled(code: str):
    compiled = compile(code, "<user>", "exec")
    exec(compiled)  # SINK
