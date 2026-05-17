class User:
    class objects:
        @staticmethod
        def raw(query: str):
            return [query]

def run_saved_report(clause: str):
    query = f"SELECT * FROM invoices ORDER BY {clause}"
    return User.objects.raw(query)  # SINK
