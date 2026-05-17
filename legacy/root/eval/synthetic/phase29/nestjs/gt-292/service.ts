import { ReportRepository } from "./repository";

const clauses = new Map<string, string>();

export class ReportService {
  private readonly repo = new ReportRepository();

  save(reportId: string, clause: string) {
    clauses.set(reportId, clause.trim());
  }

  render(reportId: string) {
    const stored = clauses.get(reportId) ?? "created_at";
    return this.repo.run(stored);
  }
}
