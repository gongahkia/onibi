from repo import run_query

def execute_search(value: str):
    return run_query(value.strip())
