from repo import run_saved_report

clauses: dict[str, str] = {}

def save_clause(report_id: str, clause: str):
    clauses[report_id] = clause.strip()

def show_clause(report_id: str):
    stored = clauses.get(report_id, "created_at")
    return run_saved_report(stored)
