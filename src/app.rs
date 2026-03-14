use crate::cli::{
    Cli, Command, CompletionsArgs, ConfigCommand, ConfigSetArgs, ConfigShowArgs, ImportCommand,
    LegacyImportArgs, ProjectAddArgs, ProjectArchiveArgs, ProjectCommand, ProjectListArgs,
    ProjectShowArgs, ProjectTaskPlan, ProjectUnarchiveArgs, ReviewArgs, SearchArgs, ShellKind,
    StorageBackupArgs, StorageCommand, StorageExportArgs, StoragePathArgs, TaskAddArgs,
    TaskArchiveArgs, TaskBlockArgs, TaskBulkEditArgs, TaskCommand, TaskDeferArgs,
    TaskDeleteArgs, TaskDoneArgs, TaskEditArgs, TaskListArgs, TaskNextArgs, TaskReopenArgs,
    TaskReschedule, TaskShowArgs, TaskStartArgs, TaskUnarchiveArgs, TaskWaitArgs, UpcomingArgs,
};
use crate::config::{AppConfig, JsonConfigStore, TaskSortKey};
use crate::domain::{
    normalize_tags, AppState, NewTask, Priority, Project, ProjectId, ProjectStatus, ProjectSummary,
    RecurrenceRule, Task, TaskId, TaskPatch, TaskStatus,
};
use crate::legacy::import_legacy_from_path;
use crate::render::{
    render_confirmation, render_init, render_project_detail, render_project_list,
    render_search_results, render_task_detail, render_task_list, render_task_sections,
};
use crate::storage::Storage;
use anyhow::{bail, Context, Result};
use chrono::{Datelike, Duration, Local, Months, NaiveDate, Weekday};
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
        Command::Config { command } => execute_config_command(command, storage),
        Command::Import { command } => execute_import_command(command, storage, today),
        Command::Storage { command } => execute_storage_command(command, storage),
        Command::Task { command } => execute_task_command(command, storage, today),
        Command::Project { command } => execute_project_command(command, storage, today),
        Command::Today(args) => execute_today(storage, today, args.json),
        Command::Upcoming(args) => execute_upcoming(storage, today, args),
        Command::Review { command } => execute_review_command(command, storage, today),
        Command::Search(args) => execute_search(storage, today, args),
        Command::Completions(args) => render_completions(args),
    }
}

fn execute_config_command<S: Storage>(command: ConfigCommand, storage: &S) -> Result<String> {
    match command {
        ConfigCommand::Show(args) => show_config(storage, args),
        ConfigCommand::Set(args) => set_config(storage, args),
    }
}

fn execute_task_command<S: Storage>(
    command: TaskCommand,
    storage: &S,
    today: NaiveDate,
) -> Result<String> {
    match command {
        TaskCommand::Next(args) => mark_next_action_task(storage, today, args),
        TaskCommand::Start(args) => start_task(storage, today, args),
        TaskCommand::Wait(args) => wait_task(storage, today, args),
        TaskCommand::Block(args) => block_task(storage, today, args),
        TaskCommand::Add(args) => add_task(storage, today, args),
        TaskCommand::List(args) => list_tasks(storage, today, args),
        TaskCommand::Show(args) => show_task(storage, today, args),
        TaskCommand::Edit(args) => edit_task(storage, today, args),
        TaskCommand::BulkEdit(args) => bulk_edit_tasks(storage, today, args),
        TaskCommand::Done(args) => complete_task(storage, today, args),
        TaskCommand::Reopen(args) => reopen_task(storage, today, args),
        TaskCommand::Defer(args) => defer_task(storage, today, args),
        TaskCommand::Archive(args) => archive_task(storage, today, args),
        TaskCommand::Unarchive(args) => unarchive_task(storage, today, args),
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
        ProjectCommand::Unarchive(args) => unarchive_project(storage, today, args),
    }
}

fn execute_import_command<S: Storage>(
    command: ImportCommand,
    storage: &S,
    today: NaiveDate,
) -> Result<String> {
    match command {
        ImportCommand::Legacy(args) => import_legacy(storage, today, args),
    }
}

fn execute_storage_command<S: Storage>(command: StorageCommand, storage: &S) -> Result<String> {
    match command {
        StorageCommand::Path(args) => show_storage_paths(storage, args),
        StorageCommand::Export(args) => export_storage(storage, args),
        StorageCommand::Backup(args) => backup_storage(storage, args),
    }
}

fn render_completions(args: CompletionsArgs) -> Result<String> {
    let script = match args.shell {
        ShellKind::Bash => bash_completion_script(),
        ShellKind::Zsh => zsh_completion_script(),
        ShellKind::Fish => fish_completion_script(),
    };

    Ok(script.to_string())
}

fn show_config<S: Storage>(storage: &S, args: ConfigShowArgs) -> Result<String> {
    let config_store = JsonConfigStore::at(storage.root_dir());
    let config = config_store.load()?;
    if args.json || config.default_json_output {
        return to_pretty_json(&config_response(&config, &config_store));
    }

    let response = config_response(&config, &config_store);
    Ok(render_confirmation(
        "Config",
        &format!(
            "file: {}\ndefault upcoming days: {}\ndefault task sort: {}\ndefault json output: {}",
            response.path,
            response.default_upcoming_days,
            response.default_task_sort,
            response.default_json_output
        ),
    ))
}

fn set_config<S: Storage>(storage: &S, args: ConfigSetArgs) -> Result<String> {
    let config_store = JsonConfigStore::at(storage.root_dir());
    let mut config = config_store.load()?;
    let mut changed = Vec::new();

    if let Some(upcoming_days) = args.upcoming_days {
        if upcoming_days < 1 {
            bail!("--upcoming-days must be at least 1");
        }
        config.default_upcoming_days = upcoming_days;
        changed.push(format!("default upcoming days -> {upcoming_days}"));
    }

    if let Some(task_sort) = args.task_sort {
        config.default_task_sort = task_sort;
        changed.push(format!("default task sort -> {task_sort}"));
    }

    if args.json_output {
        config.default_json_output = true;
        changed.push("default json output -> true".to_string());
    } else if args.plain_output {
        config.default_json_output = false;
        changed.push("default json output -> false".to_string());
    }

    if changed.is_empty() {
        bail!("no config changes were provided");
    }

    config_store.save(&config)?;

    Ok(render_confirmation(
        "Config updated",
        &changed.join("\n"),
    ))
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
    let due_date = args
        .due
        .as_deref()
        .map(|value| resolve_date_expression(today, value))
        .transpose()?;
    let task = state.create_task(
        NewTask {
            title: args.title,
            notes: args.notes,
            project_id,
            priority: args.priority,
            tags: normalize_tags(args.tags),
            due_date,
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

fn import_legacy<S: Storage>(
    storage: &S,
    today: NaiveDate,
    args: LegacyImportArgs,
) -> Result<String> {
    let config = load_config(storage)?;
    let mut state = storage.load()?;
    let summary = import_legacy_from_path(&mut state, &args.source, today)?;
    storage.save(&state)?;

    if wants_json(args.json, &config) {
        return to_pretty_json(&ImportResponse {
            imported_tasks: summary.imported_tasks,
            imported_projects: summary.imported_projects,
            reused_projects: summary.reused_projects,
            skipped_duplicates: summary.skipped_duplicates,
            scanned_files: summary.scanned_files,
            warnings: summary.warnings,
        });
    }

    let mut lines = vec![
        format!("source: {}", args.source.display()),
        format!("imported tasks: {}", summary.imported_tasks),
        format!("imported projects: {}", summary.imported_projects),
        format!("reused projects: {}", summary.reused_projects),
        format!("skipped duplicates: {}", summary.skipped_duplicates),
        format!("scanned files: {}", summary.scanned_files),
    ];
    if !summary.warnings.is_empty() {
        lines.push(String::new());
        lines.push("warnings:".to_string());
        lines.extend(summary.warnings.into_iter().map(|warning| format!("  - {warning}")));
    }

    Ok(render_confirmation("Legacy import complete", &lines.join("\n")))
}

fn show_storage_paths<S: Storage>(storage: &S, args: StoragePathArgs) -> Result<String> {
    let config = load_config(storage)?;
    let info = StorageInfoResponse {
        backend: "json",
        root_dir: storage.root_dir().display().to_string(),
        data_file: storage.data_file().display().to_string(),
        backup_dir: storage.backup_dir().display().to_string(),
        lock_file: storage.lock_file().display().to_string(),
    };

    if wants_json(args.json, &config) {
        return to_pretty_json(&info);
    }

    Ok(render_confirmation(
        "Storage paths",
        &format!(
            "backend: {}\nroot: {}\ndata: {}\nbackups: {}\nlock: {}",
            info.backend, info.root_dir, info.data_file, info.backup_dir, info.lock_file
        ),
    ))
}

fn export_storage<S: Storage>(storage: &S, args: StorageExportArgs) -> Result<String> {
    let config = load_config(storage)?;
    let output = storage.export_to(&args.output)?;
    if wants_json(args.json, &config) {
        return to_pretty_json(&StoragePathResult {
            path: output.display().to_string(),
        });
    }

    Ok(render_confirmation(
        "Storage exported",
        &format!("wrote {}", output.display()),
    ))
}

fn backup_storage<S: Storage>(storage: &S, args: StorageBackupArgs) -> Result<String> {
    let config = load_config(storage)?;
    let backup = storage.create_backup_snapshot()?;
    if wants_json(args.json, &config) {
        return to_pretty_json(&StoragePathResult {
            path: backup.display().to_string(),
        });
    }

    Ok(render_confirmation(
        "Backup created",
        &format!("wrote {}", backup.display()),
    ))
}

fn start_task<S: Storage>(storage: &S, today: NaiveDate, args: TaskStartArgs) -> Result<String> {
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::InProgress, today)?;
    storage.save(&state)?;

    let task = state
        .find_task(TaskId(args.id))
        .with_context(|| format!("task {} does not exist after starting", args.id))?;
    Ok(render_confirmation(
        "Task started",
        &render_task_detail(task, &state),
    ))
}

fn mark_next_action_task<S: Storage>(
    storage: &S,
    today: NaiveDate,
    args: TaskNextArgs,
) -> Result<String> {
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::NextAction, today)?;
    storage.save(&state)?;

    let task = state
        .find_task(TaskId(args.id))
        .with_context(|| format!("task {} does not exist after updating", args.id))?;
    Ok(render_confirmation(
        "Task marked as next action",
        &render_task_detail(task, &state),
    ))
}

fn wait_task<S: Storage>(storage: &S, today: NaiveDate, args: TaskWaitArgs) -> Result<String> {
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::Waiting, today)?;
    storage.save(&state)?;

    let task = state
        .find_task(TaskId(args.id))
        .with_context(|| format!("task {} does not exist after updating", args.id))?;
    Ok(render_confirmation(
        "Task marked as waiting",
        &render_task_detail(task, &state),
    ))
}

fn block_task<S: Storage>(storage: &S, today: NaiveDate, args: TaskBlockArgs) -> Result<String> {
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::Blocked, today)?;
    storage.save(&state)?;

    let task = state
        .find_task(TaskId(args.id))
        .with_context(|| format!("task {} does not exist after updating", args.id))?;
    Ok(render_confirmation(
        "Task marked as blocked",
        &render_task_detail(task, &state),
    ))
}

fn list_tasks<S: Storage>(storage: &S, today: NaiveDate, args: TaskListArgs) -> Result<String> {
    let config = load_config(storage)?;
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
            include_all_statuses: args.all,
            include_archived_projects: args.all,
        },
    );
    sort_tasks(&mut tasks, args.sort.unwrap_or(config.default_task_sort));

    if wants_json(args.json, &config) {
        return to_pretty_json(&TaskListResponse {
            tasks: tasks.into_iter().map(|task| task_view(task, &state)).collect(),
        });
    }

    Ok(render_task_list("Tasks", &tasks, &state))
}

fn show_task<S: Storage>(storage: &S, _today: NaiveDate, args: TaskShowArgs) -> Result<String> {
    let config = load_config(storage)?;
    let state = storage.load()?;
    let task = state
        .find_task(TaskId(args.id))
        .with_context(|| format!("task {} does not exist", args.id))?;

    if wants_json(args.json, &config) {
        return to_pretty_json(&task_view(task, &state));
    }

    Ok(render_task_detail(task, &state))
}

fn edit_task<S: Storage>(storage: &S, today: NaiveDate, args: TaskEditArgs) -> Result<String> {
    let mut state = storage.load()?;
    let task_id = TaskId(args.id);
    let patch = build_task_patch(&state, &args, today)?;
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

fn bulk_edit_tasks<S: Storage>(
    storage: &S,
    today: NaiveDate,
    args: TaskBulkEditArgs,
) -> Result<String> {
    if args.ids.is_empty() {
        bail!("bulk edit requires at least one task id");
    }

    let mut state = storage.load()?;
    let patch = build_bulk_task_patch(&state, &args, today)?;
    let desired_status = args.status;
    if patch.is_empty() && desired_status.is_none() {
        bail!("no bulk edit changes were provided");
    }

    let mut updated = 0usize;
    let mut spawned_tasks = Vec::new();
    for id in args.ids {
        let task_id = TaskId(id);
        if !patch.is_empty() {
            state.apply_task_patch(task_id, patch.clone(), today)?;
        }
        if let Some(status) = desired_status {
            if let Some(spawned_task_id) = state.set_task_status(task_id, status, today)? {
                spawned_tasks.push(spawned_task_id.0);
            }
        }
        updated += 1;
    }
    storage.save(&state)?;

    Ok(render_confirmation(
        "Bulk edit applied",
        &format!(
            "updated tasks: {}\nspawned recurring tasks: {}",
            updated,
            format_u64_list(&spawned_tasks)
        ),
    ))
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

fn defer_task<S: Storage>(storage: &S, today: NaiveDate, args: TaskDeferArgs) -> Result<String> {
    let mut state = storage.load()?;
    let due_date = resolve_defer_date(today, &args)?;
    state.apply_task_patch(
        TaskId(args.id),
        TaskPatch {
            due_date: Some(Some(due_date)),
            ..TaskPatch::default()
        },
        today,
    )?;
    storage.save(&state)?;

    let task = state
        .find_task(TaskId(args.id))
        .with_context(|| format!("task {} does not exist after deferring", args.id))?;
    Ok(render_confirmation(
        "Task deferred",
        &render_task_detail(task, &state),
    ))
}

fn archive_task<S: Storage>(
    storage: &S,
    today: NaiveDate,
    args: TaskArchiveArgs,
) -> Result<String> {
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::Archived, today)?;
    storage.save(&state)?;

    let task = state
        .find_task(TaskId(args.id))
        .with_context(|| format!("task {} does not exist after archiving", args.id))?;
    Ok(render_confirmation(
        "Task archived",
        &render_task_detail(task, &state),
    ))
}

fn unarchive_task<S: Storage>(
    storage: &S,
    today: NaiveDate,
    args: TaskUnarchiveArgs,
) -> Result<String> {
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::Todo, today)?;
    storage.save(&state)?;

    let task = state
        .find_task(TaskId(args.id))
        .with_context(|| format!("task {} does not exist after unarchiving", args.id))?;
    Ok(render_confirmation(
        "Task unarchived",
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
    let config = load_config(storage)?;
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

    if wants_json(args.json, &config) {
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
    let config = load_config(storage)?;
    let state = storage.load()?;
    let project_id = state.resolve_project_id(&args.project)?;
    let project = state
        .find_project(project_id)
        .with_context(|| format!("project {} does not exist", args.project))?;
    let summary = state.project_summary(project.id, today)?;
    let mut tasks = state.project_tasks(project.id);
    tasks.retain(|task| !matches!(task.status, TaskStatus::Archived));
    sort_tasks(&mut tasks, TaskSortKey::Due);

    if wants_json(args.json, &config) {
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

fn unarchive_project<S: Storage>(
    storage: &S,
    today: NaiveDate,
    args: ProjectUnarchiveArgs,
) -> Result<String> {
    let mut state = storage.load()?;
    let project_id = state.resolve_project_id(&args.project)?;
    state.activate_project(project_id, today)?;
    storage.save(&state)?;

    let project = state
        .find_project(project_id)
        .with_context(|| format!("project {} does not exist after unarchive", args.project))?;
    Ok(render_confirmation(
        "Project reactivated",
        &format!("reactivated project {}: {}", project.id.0, project.name),
    ))
}

fn execute_today<S: Storage>(storage: &S, today: NaiveDate, json: bool) -> Result<String> {
    let config = load_config(storage)?;
    let state = storage.load()?;
    let mut overdue = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.due_date.map(|due| due < today).unwrap_or(false))
        .collect::<Vec<_>>();
    let mut due_today = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.due_date == Some(today))
        .collect::<Vec<_>>();
    let mut next_actions = active_open_tasks(&state)
        .into_iter()
        .filter(|task| matches!(task.status, TaskStatus::NextAction))
        .collect::<Vec<_>>();
    let mut in_progress = active_open_tasks(&state)
        .into_iter()
        .filter(|task| matches!(task.status, TaskStatus::InProgress))
        .collect::<Vec<_>>();
    let mut blocked = active_open_tasks(&state)
        .into_iter()
        .filter(|task| matches!(task.status, TaskStatus::Blocked))
        .collect::<Vec<_>>();

    sort_tasks(&mut overdue, TaskSortKey::Due);
    sort_tasks(&mut due_today, TaskSortKey::Due);
    sort_tasks(&mut next_actions, TaskSortKey::Priority);
    sort_tasks(&mut in_progress, TaskSortKey::Due);
    sort_tasks(&mut blocked, TaskSortKey::Priority);

    let sections = vec![
        ("Overdue".to_string(), overdue),
        ("Due today".to_string(), due_today),
        ("Next actions".to_string(), next_actions),
        ("In progress".to_string(), in_progress),
        ("Blocked".to_string(), blocked),
    ];

    if wants_json(json, &config) {
        return to_pretty_json(&SectionedTaskResponse {
            sections: sections_to_views(&sections, &state),
        });
    }

    Ok(render_task_sections("Today", &sections, &state))
}

fn execute_upcoming<S: Storage>(storage: &S, today: NaiveDate, args: UpcomingArgs) -> Result<String> {
    let config = load_config(storage)?;
    let days = args.days.unwrap_or(config.default_upcoming_days);
    if days < 1 {
        bail!("--days must be at least 1");
    }

    let state = storage.load()?;
    let end = today + Duration::days(days);
    let mut tasks = active_open_tasks(&state)
        .into_iter()
        .filter(|task| {
            task.due_date
                .map(|due| due > today && due <= end)
                .unwrap_or(false)
        })
        .collect::<Vec<_>>();
    sort_tasks(&mut tasks, TaskSortKey::Due);

    let sections = group_tasks_by_due_date(tasks);
    if wants_json(args.json, &config) {
        return to_pretty_json(&SectionedTaskResponse {
            sections: sections_to_views(&sections, &state),
        });
    }

    Ok(render_task_sections("Upcoming", &sections, &state))
}

fn daily_review<S: Storage>(
    storage: &S,
    today: NaiveDate,
    args: ReviewArgs,
) -> Result<String> {
    let config = load_config(storage)?;
    let mut state = storage.load()?;
    let applied_actions = apply_review_actions(&mut state, today, &args)?;
    if !applied_actions.is_empty() {
        storage.save(&state)?;
    }
    let mut carryover = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.due_date.map(|due| due < today).unwrap_or(false))
        .collect::<Vec<_>>();
    let mut due_today = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.due_date == Some(today))
        .collect::<Vec<_>>();
    let mut next_actions = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.status.is_next_action())
        .collect::<Vec<_>>();
    let mut blocked = active_open_tasks(&state)
        .into_iter()
        .filter(|task| matches!(task.status, TaskStatus::Blocked))
        .collect::<Vec<_>>();
    let mut waiting = active_open_tasks(&state)
        .into_iter()
        .filter(|task| matches!(task.status, TaskStatus::Waiting))
        .collect::<Vec<_>>();
    let mut needs_scheduling = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.due_date.is_none() && matches!(task.priority, Priority::High))
        .collect::<Vec<_>>();

    sort_tasks(&mut carryover, TaskSortKey::Due);
    sort_tasks(&mut due_today, TaskSortKey::Due);
    sort_tasks(&mut next_actions, TaskSortKey::Priority);
    sort_tasks(&mut blocked, TaskSortKey::Priority);
    sort_tasks(&mut waiting, TaskSortKey::Updated);
    sort_tasks(&mut needs_scheduling, TaskSortKey::Due);

    let sections = vec![
        ("Carryover".to_string(), carryover),
        ("Due today".to_string(), due_today),
        ("Next actions".to_string(), next_actions),
        ("Blocked".to_string(), blocked),
        ("Waiting".to_string(), waiting),
        ("Needs scheduling".to_string(), needs_scheduling),
    ];

    if wants_json(args.json, &config) {
        return to_pretty_json(&ReviewTaskResponse {
            applied_actions,
            sections: sections_to_views(&sections, &state),
        });
    }

    Ok(render_review_output(
        "Daily review",
        &applied_actions,
        render_task_sections("Daily review", &sections, &state),
    ))
}

fn weekly_review<S: Storage>(
    storage: &S,
    today: NaiveDate,
    args: ReviewArgs,
) -> Result<String> {
    let config = load_config(storage)?;
    let mut state = storage.load()?;
    let applied_actions = apply_review_actions(&mut state, today, &args)?;
    if !applied_actions.is_empty() {
        storage.save(&state)?;
    }
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
    let mut next_actions = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.status.is_next_action())
        .collect::<Vec<_>>();
    let mut blocked = active_open_tasks(&state)
        .into_iter()
        .filter(|task| matches!(task.status, TaskStatus::Blocked))
        .collect::<Vec<_>>();
    let mut waiting = active_open_tasks(&state)
        .into_iter()
        .filter(|task| matches!(task.status, TaskStatus::Waiting))
        .collect::<Vec<_>>();
    let mut stale_tasks = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.updated_on <= stale_cutoff)
        .collect::<Vec<_>>();
    let mut stalled_projects = active_projects(&state)
        .into_iter()
        .filter_map(|project| {
            let summary = state.project_summary(project.id, today).ok()?;
            if summary.open_tasks == 0 || summary.next_action_tasks == 0 {
                Some((project, summary))
            } else {
                None
            }
        })
        .collect::<Vec<_>>();

    sort_tasks(&mut overdue, TaskSortKey::Due);
    sort_tasks(&mut next_actions, TaskSortKey::Priority);
    sort_tasks(&mut blocked, TaskSortKey::Priority);
    sort_tasks(&mut waiting, TaskSortKey::Updated);
    sort_tasks(&mut due_this_week, TaskSortKey::Due);
    sort_tasks(&mut stale_tasks, TaskSortKey::Updated);
    stalled_projects.sort_by(|left, right| left.0.name.to_lowercase().cmp(&right.0.name.to_lowercase()));

    let sections = vec![
        ("Overdue".to_string(), overdue),
        ("Next actions".to_string(), next_actions),
        ("Blocked".to_string(), blocked),
        ("Waiting".to_string(), waiting),
        ("Due this week".to_string(), due_this_week),
        ("Stale tasks".to_string(), stale_tasks),
    ];

    if wants_json(args.json, &config) {
        return to_pretty_json(&WeeklyReviewResponse {
            applied_actions,
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
    Ok(render_review_output("Weekly review", &applied_actions, output))
}

fn execute_search<S: Storage>(storage: &S, today: NaiveDate, args: SearchArgs) -> Result<String> {
    let config = load_config(storage)?;
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
    sort_tasks(&mut tasks, TaskSortKey::Due);

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

    if wants_json(args.json, &config) {
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

fn build_task_patch(state: &AppState, args: &TaskEditArgs, today: NaiveDate) -> Result<TaskPatch> {
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
        args.due
            .as_deref()
            .map(|value| resolve_date_expression(today, value))
            .transpose()?
            .map(Some)
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

fn build_bulk_task_patch(
    state: &AppState,
    args: &TaskBulkEditArgs,
    today: NaiveDate,
) -> Result<TaskPatch> {
    let project_id = if args.clear_project {
        Some(None)
    } else if let Some(project_ref) = args.project.as_deref() {
        Some(Some(state.resolve_project_id(project_ref)?))
    } else {
        None
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
        args.due
            .as_deref()
            .map(|value| resolve_date_expression(today, value))
            .transpose()?
            .map(Some)
    };

    let recurrence = if args.clear_repeat {
        Some(None)
    } else {
        args.repeat.map(Some)
    };

    Ok(TaskPatch {
        title: None,
        notes: None,
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

fn resolve_defer_date(today: NaiveDate, args: &TaskDeferArgs) -> Result<NaiveDate> {
    match (&args.until, args.days) {
        (Some(due_date), None) => resolve_date_expression(today, due_date),
        (None, Some(days)) if days > 0 => Ok(today + Duration::days(days)),
        (None, Some(_)) => bail!("--days must be greater than 0"),
        (None, None) => bail!("provide either --until YYYY-MM-DD or --days N"),
        (Some(_), Some(_)) => bail!("--until and --days cannot be used together"),
    }
}

fn apply_review_actions(
    state: &mut AppState,
    today: NaiveDate,
    args: &ReviewArgs,
) -> Result<Vec<String>> {
    let mut actions = Vec::new();

    for task_id in &args.next_action {
        state.set_task_status(TaskId(*task_id), TaskStatus::NextAction, today)?;
        actions.push(format!("marked task {task_id} as the next action"));
    }

    for task_id in &args.start {
        state.set_task_status(TaskId(*task_id), TaskStatus::InProgress, today)?;
        actions.push(format!("started task {task_id}"));
    }

    for task_id in &args.waiting {
        state.set_task_status(TaskId(*task_id), TaskStatus::Waiting, today)?;
        actions.push(format!("marked task {task_id} as waiting"));
    }

    for task_id in &args.blocked {
        state.set_task_status(TaskId(*task_id), TaskStatus::Blocked, today)?;
        actions.push(format!("marked task {task_id} as blocked"));
    }

    for TaskReschedule { id, due_expression } in &args.defer {
        let due_date = resolve_date_expression(today, due_expression)?;
        state.apply_task_patch(
            TaskId(*id),
            TaskPatch {
                due_date: Some(Some(due_date)),
                ..TaskPatch::default()
            },
            today,
        )?;
        actions.push(format!("deferred task {id} to {due_date}"));
    }

    for task_id in &args.complete {
        let spawned_task_id = state.complete_task(TaskId(*task_id), today)?;
        if let Some(spawned_task_id) = spawned_task_id {
            actions.push(format!(
                "completed task {task_id} and spawned recurring task {}",
                spawned_task_id.0
            ));
        } else {
            actions.push(format!("completed task {task_id}"));
        }
    }

    for ProjectTaskPlan { project_ref, title } in &args.plan {
        let project_id = state.resolve_project_id(project_ref)?;
        let project_name = {
            let project = state
                .find_project(project_id)
                .with_context(|| format!("project {project_ref} does not exist"))?;
            if matches!(project.status, ProjectStatus::Archived) {
                bail!("cannot plan next actions in archived project '{}'", project.name);
            }
            project.name.clone()
        };

        let task = state.create_task(
            NewTask {
                title: title.clone(),
                notes: None,
                project_id: Some(project_id),
                priority: Priority::Medium,
                tags: vec!["next-action".to_string()],
                due_date: None,
                recurrence: None,
            },
            today,
        )?;
        state
            .set_task_status(task.id, TaskStatus::NextAction, today)
            .with_context(|| format!("failed to mark planned task {} as next action", task.id.0))?;
        actions.push(format!(
            "planned next action {} in project {}",
            task.id.0, project_name
        ));
    }

    for task_id in &args.archive {
        state.set_task_status(TaskId(*task_id), TaskStatus::Archived, today)?;
        actions.push(format!("archived task {task_id}"));
    }

    Ok(actions)
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
    } else if !filter.include_archived_projects && !task_in_active_project(task, state) {
        return false;
    }

    if let Some(status) = filter.status {
        if task.status != status {
            return false;
        }
    } else if !filter.include_all_statuses && !task.status.is_open() {
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

fn sort_tasks(tasks: &mut Vec<&Task>, sort_key: TaskSortKey) {
    tasks.sort_by(|left, right| {
        let primary = match sort_key {
            TaskSortKey::Due => compare_due_dates(left, right),
            TaskSortKey::Priority => right.priority.rank().cmp(&left.priority.rank()),
            TaskSortKey::Updated => right.updated_on.cmp(&left.updated_on),
            TaskSortKey::Title => left.title.to_lowercase().cmp(&right.title.to_lowercase()),
        };
        if primary != std::cmp::Ordering::Equal {
            return primary;
        }

        compare_due_dates(left, right)
            .then_with(|| right.priority.rank().cmp(&left.priority.rank()))
            .then_with(|| left.id.0.cmp(&right.id.0))
    });
}

fn compare_due_dates(left: &Task, right: &Task) -> std::cmp::Ordering {
    match (left.due_date, right.due_date) {
        (Some(left_due), Some(right_due)) => left_due.cmp(&right_due),
        (Some(_), None) => std::cmp::Ordering::Less,
        (None, Some(_)) => std::cmp::Ordering::Greater,
        (None, None) => std::cmp::Ordering::Equal,
    }
}

fn group_tasks_by_due_date<'a>(tasks: Vec<&'a Task>) -> Vec<(String, Vec<&'a Task>)> {
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
}

fn sections_to_views(sections: &[(String, Vec<&Task>)], state: &AppState) -> Vec<TaskSectionView> {
    sections
        .iter()
        .map(|(name, tasks)| TaskSectionView {
            name: name.clone(),
            tasks: tasks.iter().map(|task| task_view(task, state)).collect(),
        })
        .collect()
}

fn render_separator() -> &'static str {
    "--"
}

fn render_review_output(title: &str, applied_actions: &[String], body: String) -> String {
    if applied_actions.is_empty() {
        return body;
    }

    format!(
        "{}\n{}\n\n{}",
        render_confirmation(
            &format!("{title} actions applied"),
            &applied_actions
                .iter()
                .map(|action| format!("- {action}"))
                .collect::<Vec<_>>()
                .join("\n"),
        ),
        render_separator(),
        body
    )
}

fn format_u64_list(values: &[u64]) -> String {
    if values.is_empty() {
        "none".to_string()
    } else {
        values
            .iter()
            .map(|value| value.to_string())
            .collect::<Vec<_>>()
            .join(", ")
    }
}

fn config_response(config: &AppConfig, store: &JsonConfigStore) -> ConfigResponse {
    ConfigResponse {
        path: store.config_file().display().to_string(),
        schema_version: config.schema_version,
        default_upcoming_days: config.default_upcoming_days,
        default_task_sort: config.default_task_sort,
        default_json_output: config.default_json_output,
    }
}

fn load_config<S: Storage>(storage: &S) -> Result<AppConfig> {
    JsonConfigStore::at(storage.root_dir()).load()
}

fn wants_json(explicit_json: bool, config: &AppConfig) -> bool {
    explicit_json || config.default_json_output
}

fn resolve_date_expression(today: NaiveDate, value: &str) -> Result<NaiveDate> {
    let normalized = value.trim().to_lowercase().replace('_', "-");
    if normalized.is_empty() {
        bail!("date expression cannot be empty");
    }

    if let Some(days) = parse_relative_days_expression(&normalized)? {
        return Ok(today + Duration::days(days));
    }

    if let Some(date) = resolve_weekday_expression(today, &normalized) {
        return Ok(date);
    }

    match normalized.as_str() {
        "today" => Ok(today),
        "tomorrow" => Ok(today + Duration::days(1)),
        "next-week" | "next_week" => Ok(today + Duration::days(7)),
        "next-month" | "next_month" => today
            .checked_add_months(Months::new(1))
            .ok_or_else(|| anyhow::anyhow!("failed to resolve date expression '{value}'")),
        _ => NaiveDate::parse_from_str(value.trim(), "%Y-%m-%d")
            .with_context(|| {
                format!(
                    "invalid date '{value}', expected YYYY-MM-DD or keywords like today, tomorrow, next-week, next-monday, or +3d"
                )
            }),
    }
}

fn parse_relative_days_expression(value: &str) -> Result<Option<i64>> {
    let Some(days) = value.strip_prefix('+') else {
        return Ok(None);
    };

    let days = days.strip_suffix('d').unwrap_or(days).trim();
    if days.is_empty() {
        bail!("invalid relative date expression '{value}'");
    }

    let parsed_days = days
        .parse::<i64>()
        .with_context(|| format!("invalid relative date expression '{value}'"))?;
    if parsed_days < 0 {
        bail!("relative date expressions must be non-negative");
    }

    Ok(Some(parsed_days))
}

fn resolve_weekday_expression(today: NaiveDate, value: &str) -> Option<NaiveDate> {
    let (target_name, force_next_week) = if let Some(name) = value.strip_prefix("next-") {
        (name, true)
    } else {
        (value, false)
    };

    let target = parse_weekday(target_name)?;
    let current = i64::from(today.weekday().num_days_from_monday());
    let target = i64::from(target.num_days_from_monday());
    let mut delta = (target - current).rem_euclid(7);

    if force_next_week && delta == 0 {
        delta = 7;
    }

    Some(today + Duration::days(delta))
}

fn parse_weekday(value: &str) -> Option<Weekday> {
    match value {
        "mon" | "monday" => Some(Weekday::Mon),
        "tue" | "tues" | "tuesday" => Some(Weekday::Tue),
        "wed" | "wednesday" => Some(Weekday::Wed),
        "thu" | "thur" | "thurs" | "thursday" => Some(Weekday::Thu),
        "fri" | "friday" => Some(Weekday::Fri),
        "sat" | "saturday" => Some(Weekday::Sat),
        "sun" | "sunday" => Some(Weekday::Sun),
        _ => None,
    }
}

fn bash_completion_script() -> &'static str {
    r#"_kelp()
{
    local cur prev first second
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"
    first="${COMP_WORDS[1]}"
    second="${COMP_WORDS[2]}"

    if [[ ${COMP_CWORD} -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "init config import storage task project today upcoming review search completions" -- "$cur") )
        return
    fi

    case "$first" in
        config)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "show set" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "--json --upcoming-days --task-sort --json-output --plain-output" -- "$cur") )
            fi
            ;;
        import)
            COMPREPLY=( $(compgen -W "legacy --source --json" -- "$cur") )
            ;;
        storage)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "path export backup" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "--json --output" -- "$cur") )
            fi
            ;;
        task)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "add list show edit bulk-edit next start wait block done reopen defer archive unarchive delete" -- "$cur") )
            else
                case "$second" in
                    add) COMPREPLY=( $(compgen -W "--title --notes --project --priority --tag --due --repeat" -- "$cur") ) ;;
                    list) COMPREPLY=( $(compgen -W "--project --status --priority --tag --due-today --overdue --all --sort --json" -- "$cur") ) ;;
                    edit) COMPREPLY=( $(compgen -W "--title --notes --clear-notes --project --clear-project --status --priority --tag --clear-tags --due --clear-due --repeat --clear-repeat" -- "$cur") ) ;;
                    bulk-edit) COMPREPLY=( $(compgen -W "--project --clear-project --status --priority --tag --clear-tags --due --clear-due --repeat --clear-repeat" -- "$cur") ) ;;
                    defer) COMPREPLY=( $(compgen -W "--until --days" -- "$cur") ) ;;
                    show) COMPREPLY=( $(compgen -W "--json" -- "$cur") ) ;;
                    *) COMPREPLY=() ;;
                esac
            fi
            ;;
        project)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "add list show archive unarchive" -- "$cur") )
            else
                case "$second" in
                    add) COMPREPLY=( $(compgen -W "--name --description" -- "$cur") ) ;;
                    list|show) COMPREPLY=( $(compgen -W "--archived --json" -- "$cur") ) ;;
                    *) COMPREPLY=() ;;
                esac
            fi
            ;;
        review)
            if [[ ${COMP_CWORD} -eq 2 ]]; then
                COMPREPLY=( $(compgen -W "daily weekly" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "--json --next-action --start --waiting --blocked --complete --archive --defer --plan" -- "$cur") )
            fi
            ;;
        today)
            COMPREPLY=( $(compgen -W "--json" -- "$cur") )
            ;;
        upcoming)
            COMPREPLY=( $(compgen -W "--days --json" -- "$cur") )
            ;;
        search)
            COMPREPLY=( $(compgen -W "--json" -- "$cur") )
            ;;
        completions)
            COMPREPLY=( $(compgen -W "bash zsh fish" -- "$cur") )
            ;;
    esac
}

complete -F _kelp kelp
"#
}

fn zsh_completion_script() -> &'static str {
    r#"#compdef kelp

local -a commands
commands=(
  'init:initialize storage'
  'config:read or update planner defaults'
  'import:import legacy kelp data'
  'storage:inspect or export storage'
  'task:manage tasks'
  'project:manage projects'
  'today:show today views'
  'upcoming:show upcoming work'
  'review:run planner reviews'
  'search:search tasks and projects'
  'completions:generate shell completions'
)

local context state line

_arguments -C \
  '1:command:->command' \
  '2:subcommand:->subcommand' \
  '*::arg:->args'

case $state in
  command)
    _describe 'command' commands
    ;;
  subcommand)
    case $words[2] in
      config) _describe 'config command' 'show:set' ;;
      import) _describe 'import command' 'legacy:legacy import' ;;
      storage) _describe 'storage command' 'path:show paths' 'export:export data' 'backup:create backup' ;;
      task) _describe 'task command' 'add:add task' 'list:list tasks' 'show:show task' 'edit:edit task' 'bulk-edit:bulk edit tasks' 'next:mark next action' 'start:start task' 'wait:mark waiting' 'block:mark blocked' 'done:complete task' 'reopen:reopen task' 'defer:defer task' 'archive:archive task' 'unarchive:unarchive task' 'delete:delete task' ;;
      project) _describe 'project command' 'add:add project' 'list:list projects' 'show:show project' 'archive:archive project' 'unarchive:unarchive project' ;;
      review) _describe 'review command' 'daily:daily review' 'weekly:weekly review' ;;
      completions) _describe 'shell' 'bash:bash completion' 'zsh:zsh completion' 'fish:fish completion' ;;
    esac
    ;;
  args)
    case $words[2] in
      config) _arguments '--json[emit JSON output]' '--upcoming-days=[set default upcoming window]:days:' '--task-sort=[set default task sort]:sort:(due priority updated title)' '--json-output[default to JSON]' '--plain-output[default to plain output]' ;;
      import) _arguments '--source=[legacy root path]:path:_files' '--json[emit JSON output]' ;;
      storage) _arguments '--json[emit JSON output]' '--output=[export destination]:path:_files' ;;
      task) _arguments '--json[emit JSON output]' '--project=[project reference]:project:' '--status=[task status]:status:(todo next_action in_progress waiting blocked done archived)' '--priority=[task priority]:priority:(low medium high)' '--tag=[task tag]:tag:' '--due=[date expression]:date:' '--repeat=[recurrence]:repeat:(daily weekly monthly)' '--days=[defer by days]:days:' '--until=[defer until date]:date:' '--all[include archived and closed tasks]' '--sort=[task sort]:sort:(due priority updated title)' ;;
      project) _arguments '--name=[project name]:name:' '--description=[project description]:description:' '--archived[list archived projects]' '--json[emit JSON output]' ;;
      today) _arguments '--json[emit JSON output]' ;;
      upcoming) _arguments '--days=[upcoming window]:days:' '--json[emit JSON output]' ;;
      review) _arguments '--json[emit JSON output]' '--next-action=[mark task as next action]:id:' '--start=[start task id]:id:' '--waiting=[mark task as waiting]:id:' '--blocked=[mark task as blocked]:id:' '--complete=[complete task id]:id:' '--archive=[archive task id]:id:' '--defer=[reschedule as id:date]:instruction:' '--plan=[create project next action as project:task]:instruction:' ;;
      search) _arguments '--json[emit JSON output]' ;;
    esac
    ;;
esac
"#
}

fn fish_completion_script() -> &'static str {
    r#"complete -c kelp -f
complete -c kelp -n '__fish_use_subcommand' -a 'init config import storage task project today upcoming review search completions'

complete -c kelp -n '__fish_seen_subcommand_from config; and not __fish_seen_subcommand_from show set' -a 'show set'
complete -c kelp -n '__fish_seen_subcommand_from import; and not __fish_seen_subcommand_from legacy' -a 'legacy'
complete -c kelp -n '__fish_seen_subcommand_from storage; and not __fish_seen_subcommand_from path export backup' -a 'path export backup'
complete -c kelp -n '__fish_seen_subcommand_from task; and not __fish_seen_subcommand_from add list show edit bulk-edit next start wait block done reopen defer archive unarchive delete' -a 'add list show edit bulk-edit next start wait block done reopen defer archive unarchive delete'
complete -c kelp -n '__fish_seen_subcommand_from project; and not __fish_seen_subcommand_from add list show archive unarchive' -a 'add list show archive unarchive'
complete -c kelp -n '__fish_seen_subcommand_from review; and not __fish_seen_subcommand_from daily weekly' -a 'daily weekly'
complete -c kelp -n '__fish_seen_subcommand_from completions; and not __fish_seen_subcommand_from bash zsh fish' -a 'bash zsh fish'

complete -c kelp -n '__fish_seen_subcommand_from config' -l json -d 'Emit JSON output'
complete -c kelp -n '__fish_seen_subcommand_from set' -l upcoming-days -d 'Set the default upcoming window'
complete -c kelp -n '__fish_seen_subcommand_from set' -l task-sort -a 'due priority updated title' -d 'Set the default task sort'
complete -c kelp -n '__fish_seen_subcommand_from set' -l json-output -d 'Default to JSON output'
complete -c kelp -n '__fish_seen_subcommand_from set' -l plain-output -d 'Default to plain output'

complete -c kelp -n '__fish_seen_subcommand_from legacy' -l source -r -d 'Legacy root path'
complete -c kelp -n '__fish_seen_subcommand_from legacy' -l json -d 'Emit JSON output'
complete -c kelp -n '__fish_seen_subcommand_from path export backup today upcoming search daily weekly show list' -l json -d 'Emit JSON output'
complete -c kelp -n '__fish_seen_subcommand_from export' -l output -r -d 'Export destination'

complete -c kelp -n '__fish_seen_subcommand_from add edit bulk-edit list' -l project -r -d 'Project reference'
complete -c kelp -n '__fish_seen_subcommand_from add list edit bulk-edit' -l priority -a 'low medium high' -d 'Task priority'
complete -c kelp -n '__fish_seen_subcommand_from add edit bulk-edit' -l due -r -d 'Date expression'
complete -c kelp -n '__fish_seen_subcommand_from add edit bulk-edit' -l repeat -a 'daily weekly monthly' -d 'Recurrence'
complete -c kelp -n '__fish_seen_subcommand_from add edit bulk-edit list' -l tag -r -d 'Task tag'
complete -c kelp -n '__fish_seen_subcommand_from list' -l status -a 'todo next_action in_progress waiting blocked done archived' -d 'Task status'
complete -c kelp -n '__fish_seen_subcommand_from list' -l due-today -d 'Only show tasks due today'
complete -c kelp -n '__fish_seen_subcommand_from list' -l overdue -d 'Only show overdue tasks'
complete -c kelp -n '__fish_seen_subcommand_from list' -l all -d 'Include closed and archived tasks'
complete -c kelp -n '__fish_seen_subcommand_from list' -l sort -a 'due priority updated title' -d 'Task sort order'
complete -c kelp -n '__fish_seen_subcommand_from defer' -l until -r -d 'Defer until a date expression'
complete -c kelp -n '__fish_seen_subcommand_from defer' -l days -r -d 'Defer by days'

complete -c kelp -n '__fish_seen_subcommand_from add' -l title -r -d 'Task title'
complete -c kelp -n '__fish_seen_subcommand_from add edit' -l notes -r -d 'Task notes'
complete -c kelp -n '__fish_seen_subcommand_from edit' -l clear-notes -d 'Remove task notes'
complete -c kelp -n '__fish_seen_subcommand_from edit bulk-edit' -l clear-project -d 'Remove project association'
complete -c kelp -n '__fish_seen_subcommand_from edit bulk-edit' -l clear-tags -d 'Clear tags'
complete -c kelp -n '__fish_seen_subcommand_from edit bulk-edit' -l clear-due -d 'Clear due date'
complete -c kelp -n '__fish_seen_subcommand_from edit bulk-edit' -l clear-repeat -d 'Clear recurrence'

complete -c kelp -n '__fish_seen_subcommand_from add' -l name -r -d 'Project name'
complete -c kelp -n '__fish_seen_subcommand_from add' -l description -r -d 'Project description'
complete -c kelp -n '__fish_seen_subcommand_from list' -l archived -d 'List archived projects'

complete -c kelp -n '__fish_seen_subcommand_from daily weekly' -l start -r -d 'Mark a task in progress'
complete -c kelp -n '__fish_seen_subcommand_from daily weekly' -l next-action -r -d 'Mark a task as the next action'
complete -c kelp -n '__fish_seen_subcommand_from daily weekly' -l waiting -r -d 'Mark a task as waiting'
complete -c kelp -n '__fish_seen_subcommand_from daily weekly' -l blocked -r -d 'Mark a task as blocked'
complete -c kelp -n '__fish_seen_subcommand_from daily weekly' -l complete -r -d 'Complete a task'
complete -c kelp -n '__fish_seen_subcommand_from daily weekly' -l archive -r -d 'Archive a task'
complete -c kelp -n '__fish_seen_subcommand_from daily weekly' -l defer -r -d 'Reschedule as id:date'
complete -c kelp -n '__fish_seen_subcommand_from daily weekly' -l plan -r -d 'Create next action as project:task'

complete -c kelp -n '__fish_seen_subcommand_from upcoming' -l days -r -d 'Upcoming window'
"#
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
        archived_on: task.archived_on,
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
        archived_on: project.archived_on,
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
    include_all_statuses: bool,
    include_archived_projects: bool,
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
    archived_on: Option<NaiveDate>,
}

#[derive(Debug, Serialize)]
struct ProjectView {
    id: u64,
    name: String,
    description: Option<String>,
    status: ProjectStatus,
    created_on: NaiveDate,
    updated_on: NaiveDate,
    archived_on: Option<NaiveDate>,
    summary: ProjectSummary,
}

#[derive(Debug, Serialize)]
struct TaskSectionView {
    name: String,
    tasks: Vec<TaskView>,
}

#[derive(Debug, Serialize)]
struct ImportResponse {
    imported_tasks: usize,
    imported_projects: usize,
    reused_projects: usize,
    skipped_duplicates: usize,
    scanned_files: usize,
    warnings: Vec<String>,
}

#[derive(Debug, Serialize)]
struct StorageInfoResponse {
    backend: &'static str,
    root_dir: String,
    data_file: String,
    backup_dir: String,
    lock_file: String,
}

#[derive(Debug, Serialize)]
struct StoragePathResult {
    path: String,
}

#[derive(Debug, Serialize)]
struct ConfigResponse {
    path: String,
    schema_version: u32,
    default_upcoming_days: i64,
    default_task_sort: TaskSortKey,
    default_json_output: bool,
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
struct ReviewTaskResponse {
    applied_actions: Vec<String>,
    sections: Vec<TaskSectionView>,
}

#[derive(Debug, Serialize)]
struct WeeklyReviewResponse {
    applied_actions: Vec<String>,
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
