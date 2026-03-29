use crate::cli::ColorMode;
use crate::domain::{
    AppState, Priority, Project, ProjectSummary, RecurrenceRule, Task, TaskStatus,
};
use chrono::NaiveDate;
use std::env;
use std::io::IsTerminal;
use std::path::Path;

#[derive(Debug, Clone, Copy)]
pub struct RenderOptions {
    pub color: ColorMode,
}

impl RenderOptions {
    pub fn should_colorize(self) -> bool {
        match self.color {
            ColorMode::Always => true,
            ColorMode::Never => false,
            ColorMode::Auto => std::io::stdout().is_terminal() && env::var_os("NO_COLOR").is_none(),
        }
    }
}

pub fn render_init(options: RenderOptions, path: &Path) -> String {
    format!(
        "{}\n{} {}",
        heading(options, "Kelp initialized"),
        muted(options, "data file:"),
        path.display()
    )
}

pub fn render_confirmation(options: RenderOptions, title: &str, body: &str) -> String {
    format!("{}\n{}", heading(options, title), body)
}

pub fn render_task_detail(options: RenderOptions, task: &Task, state: &AppState) -> String {
    let mut lines = vec![heading(options, &format!("Task {}", task.id.0))];
    lines.push(format!(
        "{} {}",
        muted(options, "title:"),
        bold(options, &task.title)
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "status:"),
        status_chip(options, task.status)
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "priority:"),
        priority_chip(options, task.priority)
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "project:"),
        task.project_id
            .and_then(|project_id| state.project_name(project_id))
            .unwrap_or("none")
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "due:"),
        format_optional_date(task.due_date)
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "waiting until:"),
        format_optional_date(task.waiting_until)
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "blocked reason:"),
        task.blocked_reason.as_deref().unwrap_or("none")
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "depends on:"),
        format_task_id_list(&task.depends_on)
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "repeat:"),
        task.recurrence
            .map(format_recurrence)
            .unwrap_or_else(|| "none".to_string())
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "tags:"),
        format_tags(&task.tags)
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "created:"),
        task.created_on
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "updated:"),
        task.updated_on
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "completed:"),
        task.completed_on
            .map(|date| date.to_string())
            .unwrap_or_else(|| "not completed".to_string())
    ));
    if let Some(notes) = &task.notes {
        lines.push(format!("{} {}", muted(options, "notes:"), notes));
    }

    lines.join("\n")
}

pub fn render_task_list(
    options: RenderOptions,
    title: &str,
    tasks: &[&Task],
    state: &AppState,
) -> String {
    let mut lines = vec![heading(options, title)];
    if tasks.is_empty() {
        lines.push(muted(options, "No matching tasks."));
        return lines.join("\n");
    }

    lines.push(muted(
        options,
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

pub fn render_project_list(
    options: RenderOptions,
    title: &str,
    projects: &[(&Project, ProjectSummary)],
) -> String {
    let mut lines = vec![heading(options, title)];
    if projects.is_empty() {
        lines.push(muted(options, "No matching projects."));
        return lines.join("\n");
    }

    lines.push(muted(
        options,
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
    options: RenderOptions,
    project: &Project,
    summary: ProjectSummary,
    tasks: &[&Task],
    state: &AppState,
) -> String {
    let mut lines = vec![heading(options, &format!("Project {}", project.name))];
    lines.push(format!("{} {}", muted(options, "id:"), project.id.0));
    lines.push(format!("{} {}", muted(options, "status:"), project.status));
    lines.push(format!(
        "{} {}% complete",
        muted(options, "progress:"),
        summary.completion_percent
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "open tasks:"),
        summary.open_tasks
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "done tasks:"),
        summary.completed_tasks
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "overdue tasks:"),
        summary.overdue_tasks
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "deadline:"),
        format_optional_date(project.deadline)
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "next actions:"),
        summary.next_action_tasks
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "waiting tasks:"),
        summary.waiting_tasks
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "blocked tasks:"),
        summary.blocked_tasks
    ));
    lines.push(format!(
        "{} {}",
        muted(options, "dependency blocked:"),
        summary.dependency_blocked_tasks
    ));
    if let Some(description) = &project.description {
        lines.push(format!(
            "{} {}",
            muted(options, "description:"),
            description
        ));
    }
    lines.push(String::new());
    lines.push(render_task_list(options, "Project tasks", tasks, state));

    lines.join("\n")
}

pub fn render_task_sections(
    options: RenderOptions,
    title: &str,
    sections: &[(String, Vec<&Task>)],
    state: &AppState,
) -> String {
    let mut lines = vec![heading(options, title)];
    let mut any_tasks = false;

    for (section_title, tasks) in sections {
        if tasks.is_empty() {
            continue;
        }
        any_tasks = true;
        lines.push(section(options, section_title));
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
        lines.push(muted(options, "Nothing to review."));
    }

    lines.join("\n")
}

pub fn render_search_results(
    options: RenderOptions,
    tasks: &[&Task],
    projects: &[(&Project, ProjectSummary)],
    state: &AppState,
) -> String {
    let mut lines = vec![heading(options, "Search results")];
    lines.push(format!(
        "{} {} task(s), {} project(s)",
        muted(options, "matched:"),
        tasks.len(),
        projects.len()
    ));
    lines.push(String::new());
    lines.push(render_task_list(options, "Matching tasks", tasks, state));
    lines.push(String::new());
    lines.push(render_project_list(options, "Matching projects", projects));

    lines.join("\n")
}

fn heading(options: RenderOptions, title: &str) -> String {
    accent(options, &format!("== {title} =="))
}

fn section(options: RenderOptions, title: &str) -> String {
    bold(options, &format!("-- {title} --"))
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
            .map(|task_id| task_id.to_string())
            .collect::<Vec<_>>()
            .join(", ")
    }
}

fn format_recurrence(rule: RecurrenceRule) -> String {
    rule.to_string()
}

fn truncate(value: &str, width: usize) -> String {
    if value.chars().count() <= width {
        value.to_string()
    } else {
        let mut truncated = value
            .chars()
            .take(width.saturating_sub(3))
            .collect::<String>();
        truncated.push_str("...");
        truncated
    }
}

fn priority_chip(options: RenderOptions, priority: Priority) -> String {
    match priority {
        Priority::High => danger(options, "high"),
        Priority::Medium => warning(options, "medium"),
        Priority::Low => success(options, "low"),
    }
}

fn status_chip(options: RenderOptions, status: TaskStatus) -> String {
    match status {
        TaskStatus::Todo => muted(options, "todo"),
        TaskStatus::NextAction => success(options, "next_action"),
        TaskStatus::InProgress => accent(options, "in_progress"),
        TaskStatus::Waiting => warning(options, "waiting"),
        TaskStatus::Blocked => danger(options, "blocked"),
        TaskStatus::Done => success(options, "done"),
        TaskStatus::Archived => muted(options, "archived"),
    }
}

fn accent(options: RenderOptions, value: &str) -> String {
    paint(options, value, "\u{1b}[36m")
}

fn success(options: RenderOptions, value: &str) -> String {
    paint(options, value, "\u{1b}[32m")
}

fn warning(options: RenderOptions, value: &str) -> String {
    paint(options, value, "\u{1b}[33m")
}

fn danger(options: RenderOptions, value: &str) -> String {
    paint(options, value, "\u{1b}[31m")
}

fn muted(options: RenderOptions, value: &str) -> String {
    paint(options, value, "\u{1b}[90m")
}

fn bold(options: RenderOptions, value: &str) -> String {
    paint(options, value, "\u{1b}[1m")
}

fn paint(options: RenderOptions, value: &str, code: &str) -> String {
    if options.should_colorize() {
        format!("{code}{value}\u{1b}[0m")
    } else {
        value.to_string()
    }
}
