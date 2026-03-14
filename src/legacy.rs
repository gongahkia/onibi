use crate::domain::{AppState, NewTask, Priority, ProjectId};
use anyhow::{Context, Result};
use chrono::NaiveDate;
use std::fs;
use std::path::Path;

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct LegacyImportSummary {
    pub imported_tasks: usize,
    pub imported_projects: usize,
    pub reused_projects: usize,
    pub skipped_duplicates: usize,
    pub scanned_files: usize,
    pub warnings: Vec<String>,
}

pub fn import_legacy_from_path(
    state: &mut AppState,
    source: &Path,
    today: NaiveDate,
) -> Result<LegacyImportSummary> {
    let mut summary = LegacyImportSummary::default();

    if source.is_file() {
        import_storage_file(state, source, None, today, &mut summary)?;
        return Ok(summary);
    }

    let inbox_file = source.join(".kelpStorage");
    if inbox_file.exists() {
        import_storage_file(state, &inbox_file, None, today, &mut summary)?;
    }

    let projects_dir = source.join(".kelpProjects");
    if projects_dir.is_dir() {
        let mut project_entries = fs::read_dir(&projects_dir)
            .with_context(|| format!("failed to read {}", projects_dir.display()))?
            .collect::<std::result::Result<Vec<_>, _>>()
            .with_context(|| format!("failed to enumerate {}", projects_dir.display()))?;
        project_entries.sort_by_key(|entry| entry.file_name());

        for entry in project_entries {
            let path = entry.path();
            if !path.is_dir() {
                continue;
            }

            let project_name = entry.file_name().to_string_lossy().trim().to_string();
            if project_name.is_empty() {
                continue;
            }

            let project_id = ensure_project(state, &project_name, today, &mut summary)?;
            let storage_file = path.join(".kelpStorage");
            if storage_file.exists() {
                import_storage_file(state, &storage_file, Some(project_id), today, &mut summary)?;
            }
        }
    }

    Ok(summary)
}

fn ensure_project(
    state: &mut AppState,
    project_name: &str,
    today: NaiveDate,
    summary: &mut LegacyImportSummary,
) -> Result<ProjectId> {
    if let Some((project_id, project_status)) = state
        .projects
        .iter()
        .find(|project| project.name.eq_ignore_ascii_case(project_name))
        .map(|project| (project.id, project.status))
    {
        if project_status != crate::domain::ProjectStatus::Active {
            state.activate_project(project_id, today)?;
        }
        summary.reused_projects += 1;
        return Ok(project_id);
    }

    let project = state.create_project(
        project_name.to_string(),
        Some("Imported from legacy Kelp".to_string()),
        None,
        today,
    )?;
    summary.imported_projects += 1;
    Ok(project.id)
}

fn import_storage_file(
    state: &mut AppState,
    path: &Path,
    project_id: Option<ProjectId>,
    today: NaiveDate,
    summary: &mut LegacyImportSummary,
) -> Result<()> {
    let contents =
        fs::read_to_string(path).with_context(|| format!("failed to read {}", path.display()))?;
    summary.scanned_files += 1;

    for (line_index, raw_line) in contents.lines().enumerate() {
        let line = raw_line.trim();
        if line.is_empty() {
            continue;
        }

        match parse_legacy_task_line(line) {
            Ok(task) => {
                if state
                    .tasks
                    .iter()
                    .any(|existing| task_fingerprint(existing, project_id) == legacy_task_fingerprint(&task, project_id))
                {
                    summary.skipped_duplicates += 1;
                    continue;
                }

                state.create_task(
                    NewTask {
                        title: task.title,
                        notes: task.notes,
                        project_id,
                        priority: task.priority,
                        tags: task.tags,
                        due_date: Some(task.due_date),
                        recurrence: None,
                        waiting_until: None,
                        blocked_reason: None,
                        depends_on: Vec::new(),
                    },
                    today,
                )?;
                summary.imported_tasks += 1;
            }
            Err(error) => summary.warnings.push(format!(
                "{}:{} {error}",
                path.display(),
                line_index + 1
            )),
        }
    }

    Ok(())
}

fn task_fingerprint(task: &crate::domain::Task, project_id: Option<ProjectId>) -> String {
    let mut tags = task.tags.clone();
    tags.sort();
    format!(
        "{}|{}|{}|{}|{}|{}",
        task.title.trim().to_lowercase(),
        task.notes
            .as_deref()
            .unwrap_or_default()
            .trim()
            .to_lowercase(),
        project_id
            .or(task.project_id)
            .map(|value| value.0.to_string())
            .unwrap_or_else(|| "inbox".to_string()),
        task.priority,
        task.due_date
            .map(|date| date.to_string())
            .unwrap_or_else(|| "none".to_string()),
        tags.join(",")
    )
}

fn legacy_task_fingerprint(task: &LegacyTaskRow, project_id: Option<ProjectId>) -> String {
    let mut tags = task.tags.clone();
    tags.sort();
    format!(
        "{}|{}|{}|{}|{}|{}",
        task.title.trim().to_lowercase(),
        task.notes
            .as_deref()
            .unwrap_or_default()
            .trim()
            .to_lowercase(),
        project_id
            .map(|value| value.0.to_string())
            .unwrap_or_else(|| "inbox".to_string()),
        task.priority,
        task.due_date,
        tags.join(",")
    )
}

#[derive(Debug, Clone, PartialEq, Eq)]
struct LegacyTaskRow {
    title: String,
    notes: Option<String>,
    due_date: NaiveDate,
    priority: Priority,
    tags: Vec<String>,
}

fn parse_legacy_task_line(line: &str) -> Result<LegacyTaskRow> {
    let parts: Vec<&str> = line.splitn(5, ", ").collect();
    if parts.len() != 5 {
        anyhow::bail!("expected 5 fields in legacy task row");
    }

    let due_date = parse_legacy_date(parts[2])?;
    let priority = match parts[3].trim() {
        "Low" => Priority::Low,
        "Medium" => Priority::Medium,
        "High" => Priority::High,
        other => anyhow::bail!("unknown legacy urgency '{other}'"),
    };

    let tags = parts[4]
        .split('&')
        .filter_map(|tag| {
            let cleaned = tag.trim().to_lowercase();
            if cleaned.is_empty() {
                None
            } else {
                Some(cleaned)
            }
        })
        .collect::<Vec<_>>();

    Ok(LegacyTaskRow {
        title: parts[0].trim().to_string(),
        notes: normalize_legacy_notes(parts[1]),
        due_date,
        priority,
        tags,
    })
}

fn normalize_legacy_notes(raw: &str) -> Option<String> {
    let cleaned = raw.trim().to_string();
    if cleaned.is_empty() {
        None
    } else {
        Some(cleaned)
    }
}

fn parse_legacy_date(raw: &str) -> Result<NaiveDate> {
    let fields = raw.trim_end_matches('/').split('/').collect::<Vec<_>>();
    if fields.len() != 3 {
        anyhow::bail!("invalid legacy deadline '{raw}'");
    }

    let day = fields[0]
        .trim()
        .parse::<u32>()
        .with_context(|| format!("invalid day in legacy deadline '{raw}'"))?;
    let month = fields[1]
        .trim()
        .parse::<u32>()
        .with_context(|| format!("invalid month in legacy deadline '{raw}'"))?;
    let year = fields[2]
        .trim()
        .parse::<i32>()
        .with_context(|| format!("invalid year in legacy deadline '{raw}'"))?;
    let full_year = if year < 100 { 2000 + year } else { year };

    NaiveDate::from_ymd_opt(full_year, month, day)
        .with_context(|| format!("invalid legacy deadline '{raw}'"))
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::env;
    use std::path::PathBuf;
    use std::time::{SystemTime, UNIX_EPOCH};

    fn date(value: &str) -> NaiveDate {
        NaiveDate::parse_from_str(value, "%Y-%m-%d").expect("date fixture should be valid")
    }

    fn temp_root() -> PathBuf {
        let nanos = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("system time should be after the unix epoch")
            .as_nanos();
        env::temp_dir().join(format!("kelp-legacy-test-{}-{nanos}", std::process::id()))
    }

    #[test]
    fn parses_legacy_task_rows() {
        let row = parse_legacy_task_line("Ship release, Update docs, 14/03/26/, High, ops&release")
            .expect("legacy row should parse");

        assert_eq!(row.title, "Ship release");
        assert_eq!(row.notes.as_deref(), Some("Update docs"));
        assert_eq!(row.due_date, date("2026-03-14"));
        assert_eq!(row.priority, Priority::High);
        assert_eq!(row.tags, vec!["ops".to_string(), "release".to_string()]);
    }

    #[test]
    fn imports_inbox_and_project_legacy_files() {
        let root = temp_root();
        let projects_dir = root.join(".kelpProjects").join("Launch");
        fs::create_dir_all(&projects_dir).expect("legacy project tree should be created");
        fs::write(
            root.join(".kelpStorage"),
            "Inbox task, Capture inbox notes, 15/03/26/, Medium, planning\n",
        )
        .expect("legacy inbox file should be written");
        fs::write(
            projects_dir.join(".kelpStorage"),
            "Project task, Ship launch checklist, 20/03/26/, High, launch&ops\n",
        )
        .expect("legacy project file should be written");

        let mut state = AppState::default();
        let summary =
            import_legacy_from_path(&mut state, &root, date("2026-03-14")).expect("import should succeed");

        assert_eq!(summary.imported_tasks, 2);
        assert_eq!(summary.imported_projects, 1);
        assert_eq!(summary.scanned_files, 2);
        assert_eq!(state.tasks.len(), 2);
        assert_eq!(state.projects.len(), 1);
        assert_eq!(state.tasks[1].project_id, Some(state.projects[0].id));

        fs::remove_dir_all(root).expect("temporary directory cleanup should succeed");
    }

    #[test]
    fn skips_duplicate_legacy_tasks_when_reimporting() {
        let root = temp_root();
        fs::create_dir_all(&root).expect("legacy root should be created");
        fs::write(
            root.join(".kelpStorage"),
            "Inbox task, Capture inbox notes, 15/03/26/, Medium, planning\n",
        )
        .expect("legacy inbox file should be written");

        let mut state = AppState::default();
        let first = import_legacy_from_path(&mut state, &root, date("2026-03-14"))
            .expect("first import should succeed");
        let second = import_legacy_from_path(&mut state, &root, date("2026-03-14"))
            .expect("second import should succeed");

        assert_eq!(first.imported_tasks, 1);
        assert_eq!(second.imported_tasks, 0);
        assert_eq!(second.skipped_duplicates, 1);
        assert_eq!(state.tasks.len(), 1);

        fs::remove_dir_all(root).expect("temporary directory cleanup should succeed");
    }
}
