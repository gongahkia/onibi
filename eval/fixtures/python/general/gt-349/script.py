import yaml

def parse_config(raw_yaml: str):
    return yaml.load(raw_yaml, Loader=yaml.Loader)  # SINK
