use crate::domain::{
    AppState, Priority, Project, ProjectSummary, RecurrenceRule, Task, TaskStatus,
};
use chrono::NaiveDate;
use std::env;
use std::io::IsTerminal;
use std::path::Path;

pub fn render_init(path: &Path) -> String {
    format!(
        "{}\n{} {}",
        heading("Kelp initialized"),
        muted("data file:"),
        path.display()
    )
}

pub fn render_confirmation(title: &str, body: &str) -> String {
    format!("{}\n{}", heading(title), body)
}

pub fn render_task_detail(task: &Task, state: &AppState) -> String {
    let mut lines = vec![heading(&format!("Task {}", task.id.0))];
    lines.push(format!("{} {}", muted("title:"), bold(&task.title)));
    lines.push(format!("{} {}", muted("status:"), status_chip(task.status)));
    lines.push(format!(
        "{} {}",
        muted("priority:"),
        priority_chip(task.priority)
    ));
    lines.push(format!(
        "{} {}",
        muted("project:"),
        task.project_id
            .and_then(|project_id| state.project_name(project_id))
            .unwrap_or("none")
    ));
    lines.push(format!(
        "{} {}",
        muted("due:"),
        format_optional_date(task.due_date)
    ));
    lines.push(format!(
        "{} {}",
        muted("waiting until:"),
        format_optional_date(task.waiting_until)
    ));
    lines.push(format!(
        "{} {}",
        muted("blocked reason:"),
        task.blocked_reason.as_deref().unwrap_or("none")
    ));
    lines.push(format!(
        "{} {}",
        muted("depends on:"),
        format_task_id_list(&task.depends_on)
    ));
    lines.push(format!(
        "{} {}",
        muted("repeat:"),
        task.recurrence
            .map(format_recurrence)
            .unwrap_or_else(|| "none".to_string())
    ));
    lines.push(format!("{} {}", muted("tags:"), format_tags(&task.tags)));
    lines.push(format!("{} {}", muted("created:"), task.created_on));
    lines.push(format!("{} {}", muted("updated:"), task.updated_on));
    lines.push(format!(
        "{} {}",
        muted("completed:"),
        task.completed_on
            .map(|date| date.to_string())
            .unwrap_or_else(|| "not completed".to_string())
    ));
    if let Some(notes) = &task.notes {
        lines.push(format!("{} {}", muted("notes:"), notes));
    }

    lines.join("\n")
}

pub fn render_task_list(title: &str, tasks: &[&Task], state: &AppState) -> String {
    let mut lines = vec![heading(title)];
    if tasks.is_empty() {
        lines.push(muted("No matching tasks."));
        return lines.join("\n");
    }

    lines.push(muted(
        "ID   STATUS       PRI     DUE         PROJECT       TITLE",
    ));
    for task in tasks {
        let project = task
            .project_id
            .and_then(|project_id| state.project_name(project_id))
            .unwrap_or("inbox");
        lines.push(format!(
            "{:<4} {:<12} {:<7} {:<11} {:<13} {}{}",
            task.id.0,
            task.status,
            task.priority,
            format_optional_date(task.due_date),
            truncate(project, 12),
            task.title,
            format_inline_tags(&task.tags)
        ));
    }

    lines.join("\n")
}

pub fn render_project_list(title: &str, projects: &[(&Project, ProjectSummary)]) -> String {
    let mut lines = vec![heading(title)];
    if projects.is_empty() {
        lines.push(muted("No matching projects."));
        return lines.join("\n");
    }

    lines.push(muted(
        "ID   STATUS      DONE   OPEN   OVERDUE   DEADLINE     NAME",
    ));
    for (project, summary) in projects {
        lines.push(format!(
            "{:<4} {:<11} {:>3}%   {:<6} {:<8} {:<12} {}",
            project.id.0,
            project.status,
            summary.completion_percent,
            summary.open_tasks,
            summary.overdue_tasks,
            format_optional_date(project.deadline),
            project.name
        ));
    }

    lines.join("\n")
}

pub fn render_project_detail(
    project: &Project,
    summary: ProjectSummary,
    tasks: &[&Task],
    state: &AppState,
) -> String {
    let mut lines = vec![heading(&format!("Project {}", project.name))];
    lines.push(format!("{} {}", muted("id:"), project.id.0));
    lines.push(format!("{} {}", muted("status:"), project.status));
    lines.push(format!(
        "{} {}% complete",
        muted("progress:"),
        summary.completion_percent
    ));
    lines.push(format!("{} {}", muted("open tasks:"), summary.open_tasks));
    lines.push(format!(
        "{} {}",
        muted("done tasks:"),
        summary.completed_tasks
    ));
    lines.push(format!(
        "{} {}",
        muted("overdue tasks:"),
        summary.overdue_tasks
    ));
    lines.push(format!(
        "{} {}",
        muted("deadline:"),
        format_optional_date(project.deadline)
    ));
    lines.push(format!(
        "{} {}",
        muted("next actions:"),
        summary.next_action_tasks
    ));
    lines.push(format!(
        "{} {}",
        muted("waiting tasks:"),
        summary.waiting_tasks
    ));
    lines.push(format!(
        "{} {}",
        muted("blocked tasks:"),
        summary.blocked_tasks
    ));
    lines.push(format!(
        "{} {}",
        muted("dependency blocked:"),
        summary.dependency_blocked_tasks
    ));
    if let Some(description) = &project.description {
        lines.push(format!("{} {}", muted("description:"), description));
    }
    lines.push(String::new());
    lines.push(render_task_list("Project tasks", tasks, state));

    lines.join("\n")
}

pub fn render_task_sections(
    title: &str,
    sections: &[(String, Vec<&Task>)],
    state: &AppState,
) -> String {
    let mut lines = vec![heading(title)];
    let mut any_tasks = false;

    for (section_title, tasks) in sections {
        if tasks.is_empty() {
            continue;
        }
        any_tasks = true;
        lines.push(section(section_title));
        for task in tasks {
            let project = task
                .project_id
                .and_then(|project_id| state.project_name(project_id))
                .unwrap_or("inbox");
            lines.push(format!(
                "  [{}] {:<11} {:<11} {}{}",
                task.id.0,
                format_optional_date(task.due_date),
                truncate(project, 10),
                task.title,
                format_inline_tags(&task.tags)
            ));
        }
    }

    if !any_tasks {
        lines.push(muted("Nothing to review."));
    }

    lines.join("\n")
}

pub fn render_search_results(
    tasks: &[&Task],
    projects: &[(&Project, ProjectSummary)],
    state: &AppState,
) -> String {
    let mut lines = vec![heading("Search results")];
    lines.push(format!(
        "{} {} task(s), {} project(s)",
        muted("matched:"),
        tasks.len(),
        projects.len()
    ));
    lines.push(String::new());
    lines.push(render_task_list("Matching tasks", tasks, state));
    lines.push(String::new());
    lines.push(render_project_list("Matching projects", projects));

    lines.join("\n")
}

fn heading(title: &str) -> String {
    accent(&format!("== {title} =="))
}

fn section(title: &str) -> String {
    bold(&format!("-- {title} --"))
}

fn format_inline_tags(tags: &[String]) -> String {
    if tags.is_empty() {
        String::new()
    } else {
        format!(
            "  {}",
            tags.iter()
                .map(|tag| format!("#{tag}"))
                .collect::<Vec<_>>()
                .join(" ")
        )
    }
}

fn format_tags(tags: &[String]) -> String {
    if tags.is_empty() {
        "none".to_string()
    } else {
        tags.iter()
            .map(|tag| format!("#{tag}"))
            .collect::<Vec<_>>()
            .join(" ")
    }
}

fn format_optional_date(date: Option<NaiveDate>) -> String {
    date.map(|value| value.to_string())
        .unwrap_or_else(|| "none".to_string())
}

fn format_task_id_list(task_ids: &[crate::domain::TaskId]) -> String {
    if task_ids.is_empty() {
        "none".to_string()
    } else {
        task_ids
            .iter()
            .map(|task_id| task_id.0.to_string())
            .collect::<Vec<_>>()
            .join(", ")
    }
}

fn format_recurrence(rule: RecurrenceRule) -> String {
    rule.to_string()
}

fn truncate(value: &str, width: usize) -> String {
    if value.chars().count() <= width {
        return value.to_string();
    }

    let mut truncated = value
        .chars()
        .take(width.saturating_sub(1))
        .collect::<String>();
    truncated.push('…');
    truncated
}

fn priority_chip(priority: Priority) -> String {
    match priority {
        Priority::High => danger("high"),
        Priority::Medium => warning("medium"),
        Priority::Low => success("low"),
    }
}

fn status_chip(status: TaskStatus) -> String {
    match status {
        TaskStatus::Todo => warning("todo"),
        TaskStatus::NextAction => accent("next_action"),
        TaskStatus::InProgress => accent("in_progress"),
        TaskStatus::Waiting => warning("waiting"),
        TaskStatus::Blocked => danger("blocked"),
        TaskStatus::Done => success("done"),
        TaskStatus::Archived => muted("archived"),
    }
}

fn accent(value: &str) -> String {
    paint(value, "36")
}

fn success(value: &str) -> String {
    paint(value, "32")
}

fn warning(value: &str) -> String {
    paint(value, "33")
}

fn danger(value: &str) -> String {
    paint(value, "31")
}

fn muted(value: &str) -> String {
    paint(value, "90")
}

fn bold(value: &str) -> String {
    paint(value, "1")
}

fn paint(value: &str, code: &str) -> String {
    if should_colorize() {
        format!("\u{1b}[{code}m{value}\u{1b}[0m")
    } else {
        value.to_string()
    }
}

fn should_colorize() -> bool {
    std::io::stdout().is_terminal() && env::var_os("NO_COLOR").is_none()
}
