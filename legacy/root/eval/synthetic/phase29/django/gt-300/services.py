import os

ROOT = "/srv/docs"

def scrub_name(value: str) -> str:
    return value.replace("\0", "")

def build_path(value: str) -> str:
    return os.path.join(ROOT, value)

def open_document(doc: str):
    cleaned = scrub_name(doc.strip())
    full_path = build_path(cleaned)
    return open(full_path).read()  # SINK
