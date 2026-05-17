class Cursor:
    def execute(self, query: str, params: list[str]):
        return (query, params)

cursor = Cursor()

def run_query(name: str):
    return cursor.execute("SELECT * FROM users WHERE name = %s", [name])  # SINK
