use crate::cli::{
    Cli, Command, ListOutputArgs, ProjectAddArgs, ProjectArchiveArgs, ProjectCommand,
    ProjectListArgs, ProjectShowArgs, SearchArgs, TaskAddArgs, TaskCommand, TaskDeleteArgs,
    TaskDoneArgs, TaskEditArgs, TaskListArgs, TaskReopenArgs, TaskShowArgs, UpcomingArgs,
};
use crate::domain::{
    normalize_tags, AppState, NewTask, Priority, Project, ProjectId, ProjectStatus, ProjectSummary,
    RecurrenceRule, Task, TaskId, TaskPatch, TaskStatus,
};
use crate::render::{
    render_confirmation, render_init, render_project_detail, render_project_list,
    render_search_results, render_task_detail, render_task_list, render_task_sections,
};
use crate::storage::Storage;
use anyhow::{bail, Context, Result};
use chrono::{Duration, Local, NaiveDate};
use serde::Serialize;

pub trait Clock {
    fn today(&self) -> NaiveDate;
}

#[derive(Debug, Clone, Copy)]
pub struct SystemClock;

impl Clock for SystemClock {
    fn today(&self) -> NaiveDate {
        Local::now().date_naive()
    }
}

#[derive(Debug, Clone, Copy)]
pub struct FixedClock {
    today: NaiveDate,
}

impl FixedClock {
    pub fn new(today: NaiveDate) -> Self {
        Self { today }
    }
}

impl Clock for FixedClock {
    fn today(&self) -> NaiveDate {
        self.today
    }
}

pub fn execute<S: Storage, C: Clock>(cli: Cli, storage: &S, clock: &C) -> Result<String> {
    let today = clock.today();

    match cli.command {
        Command::Init => {
            let path = storage.init()?;
            Ok(render_init(&path))
        }
        Command::Task { command } => execute_task_command(command, storage, today),
        Command::Project { command } => execute_project_command(command, storage, today),
        Command::Today(args) => execute_today(storage, today, args.json),
        Command::Upcoming(args) => execute_upcoming(storage, today, args),
        Command::Review { command } => execute_review_command(command, storage, today),
        Command::Search(args) => execute_search(storage, today, args),
    }
}

fn execute_task_command<S: Storage>(
    command: TaskCommand,
    storage: &S,
    today: NaiveDate,
) -> Result<String> {
    match command {
        TaskCommand::Add(args) => add_task(storage, today, args),
        TaskCommand::List(args) => list_tasks(storage, today, args),
        TaskCommand::Show(args) => show_task(storage, today, args),
        TaskCommand::Edit(args) => edit_task(storage, today, args),
        TaskCommand::Done(args) => complete_task(storage, today, args),
        TaskCommand::Reopen(args) => reopen_task(storage, today, args),
        TaskCommand::Delete(args) => delete_task(storage, args),
    }
}

fn execute_project_command<S: Storage>(
    command: ProjectCommand,
    storage: &S,
    today: NaiveDate,
) -> Result<String> {
    match command {
        ProjectCommand::Add(args) => add_project(storage, today, args),
        ProjectCommand::List(args) => list_projects(storage, today, args),
        ProjectCommand::Show(args) => show_project(storage, today, args),
        ProjectCommand::Archive(args) => archive_project(storage, today, args),
    }
}

fn execute_review_command<S: Storage>(
    command: crate::cli::ReviewCommand,
    storage: &S,
    today: NaiveDate,
) -> Result<String> {
    match command {
        crate::cli::ReviewCommand::Daily(args) => daily_review(storage, today, args),
        crate::cli::ReviewCommand::Weekly(args) => weekly_review(storage, today, args),
    }
}

fn add_task<S: Storage>(storage: &S, today: NaiveDate, args: TaskAddArgs) -> Result<String> {
    let mut state = storage.load()?;
    let project_id = resolve_optional_project_id(&state, args.project.as_deref())?;
    let task = state.create_task(
        NewTask {
            title: args.title,
            notes: args.notes,
            project_id,
            priority: args.priority,
            tags: normalize_tags(args.tags),
            due_date: args.due,
            recurrence: args.repeat,
        },
        today,
    )?;
    storage.save(&state)?;

    Ok(render_confirmation(
        "Task created",
        &render_task_detail(&task, &state),
    ))
}

fn list_tasks<S: Storage>(storage: &S, today: NaiveDate, args: TaskListArgs) -> Result<String> {
    let state = storage.load()?;
    let project_id = resolve_optional_project_id(&state, args.project.as_deref())?;
    let mut tasks = filtered_tasks(
        &state,
        today,
        TaskFilter {
            project_id,
            status: args.status,
            priority: args.priority,
            tag: args.tag.as_deref(),
            due_today: args.due_today,
            overdue: args.overdue,
        },
    );
    sort_tasks(&mut tasks);

    if args.json {
        return to_pretty_json(&TaskListResponse {
            tasks: tasks.into_iter().map(|task| task_view(task, &state)).collect(),
        });
    }

    Ok(render_task_list("Tasks", &tasks, &state))
}

fn show_task<S: Storage>(storage: &S, _today: NaiveDate, args: TaskShowArgs) -> Result<String> {
    let state = storage.load()?;
    let task = state
        .find_task(TaskId(args.id))
        .with_context(|| format!("task {} does not exist", args.id))?;

    if args.json {
        return to_pretty_json(&task_view(task, &state));
    }

    Ok(render_task_detail(task, &state))
}

fn edit_task<S: Storage>(storage: &S, today: NaiveDate, args: TaskEditArgs) -> Result<String> {
    let mut state = storage.load()?;
    let task_id = TaskId(args.id);
    let patch = build_task_patch(&state, &args)?;
    let desired_status = args.status;
    if patch.is_empty() && desired_status.is_none() {
        bail!("no task changes were provided");
    }

    if !patch.is_empty() {
        state.apply_task_patch(task_id, patch, today)?;
    }
    let spawned_task_id = if let Some(status) = desired_status {
        state.set_task_status(task_id, status, today)?
    } else {
        None
    };
    storage.save(&state)?;

    let task = state
        .find_task(task_id)
        .with_context(|| format!("task {} does not exist after update", args.id))?;
    let mut output = render_confirmation("Task updated", &render_task_detail(task, &state));
    if let Some(next_task_id) = spawned_task_id {
        output.push_str(&format!(
            "\n{}\nspawned recurring task {}",
            render_separator(),
            next_task_id.0
        ));
    }

    Ok(output)
}

fn complete_task<S: Storage>(storage: &S, today: NaiveDate, args: TaskDoneArgs) -> Result<String> {
    let mut state = storage.load()?;
    let spawned_task_id = state.complete_task(TaskId(args.id), today)?;
    storage.save(&state)?;

    let task = state
        .find_task(TaskId(args.id))
        .with_context(|| format!("task {} does not exist after completion", args.id))?;
    let mut output = render_confirmation("Task completed", &render_task_detail(task, &state));
    if let Some(next_task_id) = spawned_task_id {
        output.push_str(&format!(
            "\n{}\nspawned recurring task {}",
            render_separator(),
            next_task_id.0
        ));
    }

    Ok(output)
}

fn reopen_task<S: Storage>(storage: &S, today: NaiveDate, args: TaskReopenArgs) -> Result<String> {
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::Todo, today)?;
    storage.save(&state)?;

    let task = state
        .find_task(TaskId(args.id))
        .with_context(|| format!("task {} does not exist after reopening", args.id))?;
    Ok(render_confirmation(
        "Task reopened",
        &render_task_detail(task, &state),
    ))
}

fn delete_task<S: Storage>(storage: &S, args: TaskDeleteArgs) -> Result<String> {
    let mut state = storage.load()?;
    let task = state.delete_task(TaskId(args.id))?;
    storage.save(&state)?;

    Ok(render_confirmation(
        "Task deleted",
        &format!("removed task {}: {}", task.id.0, task.title),
    ))
}

fn add_project<S: Storage>(storage: &S, today: NaiveDate, args: ProjectAddArgs) -> Result<String> {
    let mut state = storage.load()?;
    let project = state.create_project(args.name, args.description, today)?;
    storage.save(&state)?;

    Ok(render_confirmation(
        "Project created",
        &format!("created project {}: {}", project.id.0, project.name),
    ))
}

fn list_projects<S: Storage>(
    storage: &S,
    today: NaiveDate,
    args: ProjectListArgs,
) -> Result<String> {
    let state = storage.load()?;
    let mut projects: Vec<&Project> = state
        .projects
        .iter()
        .filter(|project| match (args.archived, project.status) {
            (true, ProjectStatus::Archived) => true,
            (true, _) => false,
            (false, ProjectStatus::Active) => true,
            (false, ProjectStatus::Archived) => false,
        })
        .collect();
    projects.sort_by(|left, right| left.name.to_lowercase().cmp(&right.name.to_lowercase()));

    let project_entries: Vec<(&Project, ProjectSummary)> = projects
        .iter()
        .map(|project| {
            let summary = state.project_summary(project.id, today)?;
            Ok((*project, summary))
        })
        .collect::<Result<_>>()?;

    if args.json {
        return to_pretty_json(&ProjectListResponse {
            projects: project_entries
                .iter()
                .map(|(project, summary)| project_view(project, *summary))
                .collect(),
        });
    }

    Ok(render_project_list("Projects", &project_entries))
}

fn show_project<S: Storage>(storage: &S, today: NaiveDate, args: ProjectShowArgs) -> Result<String> {
    let state = storage.load()?;
    let project_id = state.resolve_project_id(&args.project)?;
    let project = state
        .find_project(project_id)
        .with_context(|| format!("project {} does not exist", args.project))?;
    let summary = state.project_summary(project.id, today)?;
    let mut tasks = state.project_tasks(project.id);
    tasks.retain(|task| !matches!(task.status, TaskStatus::Archived));
    sort_tasks(&mut tasks);

    if args.json {
        return to_pretty_json(&ProjectDetailResponse {
            project: project_view(project, summary),
            tasks: tasks.into_iter().map(|task| task_view(task, &state)).collect(),
        });
    }

    Ok(render_project_detail(project, summary, &tasks, &state))
}

fn archive_project<S: Storage>(
    storage: &S,
    today: NaiveDate,
    args: ProjectArchiveArgs,
) -> Result<String> {
    let mut state = storage.load()?;
    let project_id = state.resolve_project_id(&args.project)?;
    state.archive_project(project_id, today)?;
    storage.save(&state)?;

    let project = state
        .find_project(project_id)
        .with_context(|| format!("project {} does not exist after archive", args.project))?;
    Ok(render_confirmation(
        "Project archived",
        &format!("archived project {}: {}", project.id.0, project.name),
    ))
}

fn execute_today<S: Storage>(storage: &S, today: NaiveDate, json: bool) -> Result<String> {
    let state = storage.load()?;
    let mut overdue = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.due_date.map(|due| due < today).unwrap_or(false))
        .collect::<Vec<_>>();
    let mut due_today = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.due_date == Some(today))
        .collect::<Vec<_>>();
    let mut in_progress = active_open_tasks(&state)
        .into_iter()
        .filter(|task| matches!(task.status, TaskStatus::InProgress))
        .collect::<Vec<_>>();

    sort_tasks(&mut overdue);
    sort_tasks(&mut due_today);
    sort_tasks(&mut in_progress);

    let sections = vec![
        ("Overdue", overdue),
        ("Due today", due_today),
        ("In progress", in_progress),
    ];

    if json {
        return to_pretty_json(&SectionedTaskResponse {
            sections: sections_to_views(&sections, &state),
        });
    }

    Ok(render_task_sections("Today", &sections, &state))
}

fn execute_upcoming<S: Storage>(storage: &S, today: NaiveDate, args: UpcomingArgs) -> Result<String> {
    if args.days < 1 {
        bail!("--days must be at least 1");
    }

    let state = storage.load()?;
    let end = today + Duration::days(args.days);
    let mut tasks = active_open_tasks(&state)
        .into_iter()
        .filter(|task| {
            task.due_date
                .map(|due| due > today && due <= end)
                .unwrap_or(false)
        })
        .collect::<Vec<_>>();
    sort_tasks(&mut tasks);

    let sections = group_tasks_by_due_date(tasks);
    if args.json {
        return to_pretty_json(&SectionedTaskResponse {
            sections: sections_to_views(&sections, &state),
        });
    }

    Ok(render_task_sections("Upcoming", &sections, &state))
}

fn daily_review<S: Storage>(
    storage: &S,
    today: NaiveDate,
    args: ListOutputArgs,
) -> Result<String> {
    let state = storage.load()?;
    let mut carryover = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.due_date.map(|due| due < today).unwrap_or(false))
        .collect::<Vec<_>>();
    let mut due_today = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.due_date == Some(today))
        .collect::<Vec<_>>();
    let mut needs_scheduling = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.due_date.is_none() && matches!(task.priority, Priority::High))
        .collect::<Vec<_>>();

    sort_tasks(&mut carryover);
    sort_tasks(&mut due_today);
    sort_tasks(&mut needs_scheduling);

    let sections = vec![
        ("Carryover", carryover),
        ("Due today", due_today),
        ("Needs scheduling", needs_scheduling),
    ];

    if args.json {
        return to_pretty_json(&SectionedTaskResponse {
            sections: sections_to_views(&sections, &state),
        });
    }

    Ok(render_task_sections("Daily review", &sections, &state))
}

fn weekly_review<S: Storage>(
    storage: &S,
    today: NaiveDate,
    args: ListOutputArgs,
) -> Result<String> {
    let state = storage.load()?;
    let window_end = today + Duration::days(7);
    let stale_cutoff = today - Duration::days(7);

    let mut overdue = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.due_date.map(|due| due < today).unwrap_or(false))
        .collect::<Vec<_>>();
    let mut due_this_week = active_open_tasks(&state)
        .into_iter()
        .filter(|task| {
            task.due_date
                .map(|due| due >= today && due <= window_end)
                .unwrap_or(false)
        })
        .collect::<Vec<_>>();
    let mut stale_tasks = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.updated_on <= stale_cutoff)
        .collect::<Vec<_>>();
    let mut stalled_projects = active_projects(&state)
        .into_iter()
        .filter_map(|project| {
            let summary = state.project_summary(project.id, today).ok()?;
            if summary.open_tasks == 0 {
                Some((project, summary))
            } else {
                None
            }
        })
        .collect::<Vec<_>>();

    sort_tasks(&mut overdue);
    sort_tasks(&mut due_this_week);
    sort_tasks(&mut stale_tasks);
    stalled_projects.sort_by(|left, right| left.0.name.to_lowercase().cmp(&right.0.name.to_lowercase()));

    let sections = vec![
        ("Overdue", overdue),
        ("Due this week", due_this_week),
        ("Stale tasks", stale_tasks),
    ];

    if args.json {
        return to_pretty_json(&WeeklyReviewResponse {
            sections: sections_to_views(&sections, &state),
            stalled_projects: stalled_projects
                .iter()
                .map(|(project, summary)| project_view(project, *summary))
                .collect(),
        });
    }

    let mut output = render_task_sections("Weekly review", &sections, &state);
    output.push_str("\n\n");
    output.push_str(&render_project_list(
        "Projects without next actions",
        &stalled_projects,
    ));
    Ok(output)
}

fn execute_search<S: Storage>(storage: &S, today: NaiveDate, args: SearchArgs) -> Result<String> {
    let state = storage.load()?;
    let query = args.query.trim();
    if query.is_empty() {
        bail!("search query cannot be empty");
    }

    let mut tasks = state
        .tasks
        .iter()
        .filter(|task| {
            !matches!(task.status, TaskStatus::Archived)
                && task_in_active_project(task, &state)
                && (task.matches_query(query)
                    || task
                        .project_id
                        .and_then(|project_id| state.project_name(project_id))
                        .map(|project_name| project_name.to_lowercase().contains(&query.to_lowercase()))
                        .unwrap_or(false))
        })
        .collect::<Vec<_>>();
    sort_tasks(&mut tasks);

    let mut projects = active_projects(&state)
        .into_iter()
        .filter_map(|project| {
            if project.matches_query(query) {
                let summary = state.project_summary(project.id, today).ok()?;
                Some((project, summary))
            } else {
                None
            }
        })
        .collect::<Vec<_>>();
    projects.sort_by(|left, right| left.0.name.to_lowercase().cmp(&right.0.name.to_lowercase()));

    if args.json {
        return to_pretty_json(&SearchResponse {
            tasks: tasks.iter().map(|task| task_view(task, &state)).collect(),
            projects: projects
                .iter()
                .map(|(project, summary)| project_view(project, *summary))
                .collect(),
        });
    }

    Ok(render_search_results(&tasks, &projects, &state))
}

fn build_task_patch(state: &AppState, args: &TaskEditArgs) -> Result<TaskPatch> {
    let project_id = if args.clear_project {
        Some(None)
    } else if let Some(project_ref) = args.project.as_deref() {
        Some(Some(state.resolve_project_id(project_ref)?))
    } else {
        None
    };

    let notes = if args.clear_notes {
        Some(None)
    } else {
        args.notes.clone().map(Some)
    };

    let tags = if args.clear_tags {
        Some(Vec::new())
    } else if args.tags.is_empty() {
        None
    } else {
        Some(normalize_tags(args.tags.clone()))
    };

    let due_date = if args.clear_due {
        Some(None)
    } else {
        args.due.map(Some)
    };

    let recurrence = if args.clear_repeat {
        Some(None)
    } else {
        args.repeat.map(Some)
    };

    Ok(TaskPatch {
        title: args.title.clone(),
        notes,
        project_id,
        status: None,
        priority: args.priority,
        tags,
        due_date,
        recurrence,
    })
}

fn resolve_optional_project_id(state: &AppState, project_ref: Option<&str>) -> Result<Option<ProjectId>> {
    project_ref
        .map(|reference| state.resolve_project_id(reference))
        .transpose()
        .map_err(Into::into)
}

fn filtered_tasks<'a>(
    state: &'a AppState,
    today: NaiveDate,
    filter: TaskFilter<'_>,
) -> Vec<&'a Task> {
    state
        .tasks
        .iter()
        .filter(|task| task_matches_filter(task, state, today, &filter))
        .collect()
}

fn task_matches_filter(task: &Task, state: &AppState, today: NaiveDate, filter: &TaskFilter<'_>) -> bool {
    if let Some(project_id) = filter.project_id {
        if task.project_id != Some(project_id) {
            return false;
        }
    } else if !task_in_active_project(task, state) {
        return false;
    }

    if let Some(status) = filter.status {
        if task.status != status {
            return false;
        }
    } else if !task.status.is_open() {
        return false;
    }

    if let Some(priority) = filter.priority {
        if task.priority != priority {
            return false;
        }
    }

    if let Some(tag) = filter.tag {
        if !task.has_tag(tag) {
            return false;
        }
    }

    if filter.due_today && task.due_date != Some(today) {
        return false;
    }

    if filter.overdue
        && !task
            .due_date
            .map(|due_date| due_date < today)
            .unwrap_or(false)
    {
        return false;
    }

    true
}

fn active_open_tasks(state: &AppState) -> Vec<&Task> {
    state
        .tasks
        .iter()
        .filter(|task| task.status.is_open() && task_in_active_project(task, state))
        .collect()
}

fn active_projects(state: &AppState) -> Vec<&Project> {
    state
        .projects
        .iter()
        .filter(|project| matches!(project.status, ProjectStatus::Active))
        .collect()
}

fn task_in_active_project(task: &Task, state: &AppState) -> bool {
    task.project_id
        .map(|project_id| !state.is_project_archived(project_id))
        .unwrap_or(true)
}

fn sort_tasks(tasks: &mut Vec<&Task>) {
    tasks.sort_by(|left, right| {
        let due_order = match (left.due_date, right.due_date) {
            (Some(left_due), Some(right_due)) => left_due.cmp(&right_due),
            (Some(_), None) => std::cmp::Ordering::Less,
            (None, Some(_)) => std::cmp::Ordering::Greater,
            (None, None) => std::cmp::Ordering::Equal,
        };
        if due_order != std::cmp::Ordering::Equal {
            return due_order;
        }

        right
            .priority
            .rank()
            .cmp(&left.priority.rank())
            .then_with(|| left.id.0.cmp(&right.id.0))
    });
}

fn group_tasks_by_due_date<'a>(tasks: Vec<&'a Task>) -> Vec<(&'static str, Vec<&'a Task>)> {
    let mut grouped: Vec<(String, Vec<&Task>)> = Vec::new();

    for task in tasks {
        let label = task
            .due_date
            .map(|date| date.to_string())
            .unwrap_or_else(|| "No due date".to_string());
        if let Some((_, items)) = grouped.iter_mut().find(|(existing, _)| existing == &label) {
            items.push(task);
        } else {
            grouped.push((label, vec![task]));
        }
    }

    grouped
        .into_iter()
        .map(|(label, items)| (Box::leak(label.into_boxed_str()) as &'static str, items))
        .collect()
}

fn sections_to_views(sections: &[(&str, Vec<&Task>)], state: &AppState) -> Vec<TaskSectionView> {
    sections
        .iter()
        .map(|(name, tasks)| TaskSectionView {
            name: (*name).to_string(),
            tasks: tasks.iter().map(|task| task_view(task, state)).collect(),
        })
        .collect()
}

fn render_separator() -> &'static str {
    "--"
}

fn task_view(task: &Task, state: &AppState) -> TaskView {
    TaskView {
        id: task.id.0,
        title: task.title.clone(),
        notes: task.notes.clone(),
        project: task
            .project_id
            .and_then(|project_id| state.project_name(project_id).map(str::to_string)),
        status: task.status,
        priority: task.priority,
        tags: task.tags.clone(),
        due_date: task.due_date,
        recurrence: task.recurrence,
        created_on: task.created_on,
        updated_on: task.updated_on,
        completed_on: task.completed_on,
    }
}

fn project_view(project: &Project, summary: ProjectSummary) -> ProjectView {
    ProjectView {
        id: project.id.0,
        name: project.name.clone(),
        description: project.description.clone(),
        status: project.status,
        created_on: project.created_on,
        updated_on: project.updated_on,
        summary,
    }
}

fn to_pretty_json<T: Serialize>(value: &T) -> Result<String> {
    Ok(serde_json::to_string_pretty(value)?)
}

struct TaskFilter<'a> {
    project_id: Option<ProjectId>,
    status: Option<TaskStatus>,
    priority: Option<Priority>,
    tag: Option<&'a str>,
    due_today: bool,
    overdue: bool,
}

#[derive(Debug, Serialize)]
struct TaskView {
    id: u64,
    title: String,
    notes: Option<String>,
    project: Option<String>,
    status: TaskStatus,
    priority: Priority,
    tags: Vec<String>,
    due_date: Option<NaiveDate>,
    recurrence: Option<RecurrenceRule>,
    created_on: NaiveDate,
    updated_on: NaiveDate,
    completed_on: Option<NaiveDate>,
}

#[derive(Debug, Serialize)]
struct ProjectView {
    id: u64,
    name: String,
    description: Option<String>,
    status: ProjectStatus,
    created_on: NaiveDate,
    updated_on: NaiveDate,
    summary: ProjectSummary,
}

#[derive(Debug, Serialize)]
struct TaskSectionView {
    name: String,
    tasks: Vec<TaskView>,
}

#[derive(Debug, Serialize)]
struct TaskListResponse {
    tasks: Vec<TaskView>,
}

#[derive(Debug, Serialize)]
struct ProjectListResponse {
    projects: Vec<ProjectView>,
}

#[derive(Debug, Serialize)]
struct ProjectDetailResponse {
    project: ProjectView,
    tasks: Vec<TaskView>,
}

#[derive(Debug, Serialize)]
struct SectionedTaskResponse {
    sections: Vec<TaskSectionView>,
}

#[derive(Debug, Serialize)]
struct WeeklyReviewResponse {
    sections: Vec<TaskSectionView>,
    stalled_projects: Vec<ProjectView>,
}

#[derive(Debug, Serialize)]
struct SearchResponse {
    tasks: Vec<TaskView>,
    projects: Vec<ProjectView>,
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::cli::Cli;
    use crate::storage::JsonFileStorage;
    use clap::Parser;
    use std::env;
    use std::fs;
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
        env::temp_dir().join(format!("kelp-app-test-{}-{nanos}", std::process::id()))
    }

    fn run(args: &[&str], storage: &JsonFileStorage, clock: &FixedClock) -> String {
        let cli = Cli::parse_from(args);
        execute(cli, storage, clock).expect("command should succeed")
    }

    #[test]
    fn recurring_tasks_show_up_in_upcoming_after_completion() {
        let root = temp_root();
        let storage = JsonFileStorage::at(root.clone());
        let clock = FixedClock::new(date("2026-03-14"));

        run(
            &[
                "kelp",
                "task",
                "add",
                "--title",
                "Weekly review",
                "--due",
                "2026-03-14",
                "--repeat",
                "weekly",
            ],
            &storage,
            &clock,
        );
        run(&["kelp", "task", "done", "1"], &storage, &clock);
        let upcoming = run(
            &["kelp", "upcoming", "--days", "14", "--json"],
            &storage,
            &clock,
        );

        assert!(upcoming.contains("\"id\": 2"));
        assert!(upcoming.contains("\"due_date\": \"2026-03-21\""));

        fs::remove_dir_all(root).expect("temporary directory cleanup should succeed");
    }

    #[test]
    fn daily_review_groups_overdue_and_due_today_work() {
        let root = temp_root();
        let storage = JsonFileStorage::at(root.clone());
        let clock = FixedClock::new(date("2026-03-14"));

        run(
            &[
                "kelp",
                "task",
                "add",
                "--title",
                "Missed task",
                "--due",
                "2026-03-10",
            ],
            &storage,
            &clock,
        );
        run(
            &[
                "kelp",
                "task",
                "add",
                "--title",
                "Today task",
                "--due",
                "2026-03-14",
            ],
            &storage,
            &clock,
        );
        let review = run(&["kelp", "review", "daily", "--json"], &storage, &clock);

        assert!(review.contains("\"name\": \"Carryover\""));
        assert!(review.contains("\"name\": \"Due today\""));
        assert!(review.contains("Missed task"));
        assert!(review.contains("Today task"));

        fs::remove_dir_all(root).expect("temporary directory cleanup should succeed");
    }
}
