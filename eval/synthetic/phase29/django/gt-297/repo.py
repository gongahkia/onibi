class User:
    class objects:
        @staticmethod
        def raw(query: str):
            return [query]

def run_raw_lookup(name: str):
    query = f"SELECT * FROM users WHERE name = '{name}'"
    return User.objects.raw(query)  # SINK
