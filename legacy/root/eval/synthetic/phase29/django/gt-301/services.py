from client import fetch_remote

def normalize_url(value: str) -> str:
    return str(value.strip())

def proxy_url(value: str):
    endpoint = normalize_url(value)
    return fetch_remote(endpoint)
