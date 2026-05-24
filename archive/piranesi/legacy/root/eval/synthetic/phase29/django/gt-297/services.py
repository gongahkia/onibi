from repo import run_raw_lookup

def normalize_name(value: str) -> str:
    return value.strip()

def execute_search(value: str):
    prepared = normalize_name(value)
    return run_raw_lookup(prepared)
