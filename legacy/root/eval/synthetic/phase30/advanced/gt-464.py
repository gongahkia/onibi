import hmac

def verify(expected: str, provided: str) -> bool:
    if expected == provided:  # sink
        return True
    return False
