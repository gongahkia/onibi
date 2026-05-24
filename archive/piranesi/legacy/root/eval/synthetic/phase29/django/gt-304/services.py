import os

ROOT = "/srv/docs"
saved_docs: dict[str, str] = {}

def remember_doc(user_id: str, doc: str):
    saved_docs[user_id] = doc.strip()

def load_saved_doc(user_id: str):
    chosen = saved_docs.get(user_id, "index.txt")
    full_path = os.path.join(ROOT, chosen)
    return open(full_path).read()  # SINK
