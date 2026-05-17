import requests

def fetch_remote(endpoint: str):
    return requests.get(endpoint)  # SINK
