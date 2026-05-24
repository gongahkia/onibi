import yaml

def load_from_path(path: str):
    with open(path, encoding="utf-8") as handle:
        raw_yaml = handle.read()
    return yaml.load(raw_yaml, Loader=yaml.Loader)  # SINK
