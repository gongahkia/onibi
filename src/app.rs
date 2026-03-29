use crate::cli::{
    Cli, Command, CompletionsArgs, ConfigCommand, ConfigSetArgs, ConfigShowArgs, ImportCommand,
    LegacyImportArgs, OutputFormat, ProjectAddArgs, ProjectArchiveArgs, ProjectCommand,
    ProjectEditArgs, ProjectListArgs, ProjectShowArgs, ProjectTaskPlan, ProjectUnarchiveArgs,
    ReviewArgs, SearchArgs, ShellKind, StorageBackupArgs, StorageCommand, StorageExportArgs,
    StoragePathArgs, TaskAddArgs, TaskArchiveArgs, TaskBlockArgs, TaskBulkEditArgs, TaskCommand,
    TaskDeferArgs, TaskDeleteArgs, TaskDoneArgs, TaskEditArgs, TaskListArgs, TaskNextArgs,
    TaskReadyArgs, TaskReopenArgs, TaskReschedule, TaskShowArgs, TaskStartArgs, TaskUnarchiveArgs,
    TaskWaitArgs, UpcomingArgs,
};
use crate::config::{AppConfig, JsonConfigStore, TaskSortKey};
use crate::domain::{
    normalize_tags, AppState, NewTask, Priority, Project, ProjectId, ProjectPatch, ProjectStatus,
    ProjectSummary, RecurrenceRule, Task, TaskId, TaskPatch, TaskStatus,
};
use crate::error::{conflict_error, not_found_error, usage_error};
use crate::legacy::import_legacy_from_path;
use crate::output::success_json;
use crate::render::{
    render_confirmation, render_init, render_project_detail, render_project_list,
    render_search_results, render_task_detail, render_task_list, render_task_sections,
    RenderOptions,
};
use crate::storage::Storage;
use anyhow::{Context, Result};
use chrono::{Datelike, Duration, Local, Months, NaiveDate, Weekday};
use clap::CommandFactory;
use clap_complete::{generate, shells};
use serde::Serialize;
use std::fs;
use std::io::Cursor;
use std::path::Path;

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

#[derive(Debug, Clone, Copy)]
pub struct RuntimeOptions {
    render: RenderOptions,
    requested_output: Option<OutputFormat>,
}

impl RuntimeOptions {
    fn from_cli(cli: &Cli) -> Self {
        Self {
            render: RenderOptions { color: cli.color },
            requested_output: cli.requested_output(),
        }
    }

    fn wants_json(self, config: &AppConfig) -> bool {
        matches!(self.requested_output, Some(OutputFormat::Json))
            || (self.requested_output.is_none() && config.default_json_output)
    }
}

pub fn execute<S: Storage, C: Clock>(cli: Cli, storage: &S, clock: &C) -> Result<String> {
    let config_store = JsonConfigStore::at(storage.root_dir());
    execute_with_config(cli, storage, &config_store, clock)
}

pub fn execute_with_config<S: Storage, C: Clock>(
    cli: Cli,
    storage: &S,
    config_store: &JsonConfigStore,
    clock: &C,
) -> Result<String> {
    let today = clock.today();
    let runtime = RuntimeOptions::from_cli(&cli);

    match cli.command {
        Command::Init => {
            let path = storage.init()?;
            let config = load_config(config_store)?;
            if runtime.wants_json(&config) {
                return to_pretty_json(
                    "init",
                    &StoragePathResult {
                        path: path.display().to_string(),
                    },
                );
            }
            Ok(render_init(runtime.render, &path))
        }
        Command::Config { command } => execute_config_command(command, config_store, runtime),
        Command::Import { command } => {
            execute_import_command(command, storage, config_store, today, runtime)
        }
        Command::Storage { command } => {
            execute_storage_command(command, storage, config_store, runtime)
        }
        Command::Task { command } => {
            execute_task_command(command, storage, config_store, today, runtime)
        }
        Command::Project { command } => {
            execute_project_command(command, storage, config_store, today, runtime)
        }
        Command::Today(args) => execute_today(storage, config_store, today, args, runtime),
        Command::Upcoming(args) => execute_upcoming(storage, config_store, today, args, runtime),
        Command::Review { command } => {
            execute_review_command(command, storage, config_store, today, runtime)
        }
        Command::Search(args) => execute_search(storage, config_store, today, args, runtime),
        Command::Completions(args) => render_completions(args),
    }
}

fn execute_config_command(
    command: ConfigCommand,
    config_store: &JsonConfigStore,
    runtime: RuntimeOptions,
) -> Result<String> {
    match command {
        ConfigCommand::Show(args) => show_config(config_store, args, runtime),
        ConfigCommand::Set(args) => set_config(config_store, args, runtime),
    }
}

fn execute_task_command<S: Storage>(
    command: TaskCommand,
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    runtime: RuntimeOptions,
) -> Result<String> {
    match command {
        TaskCommand::Next(args) => {
            mark_next_action_task(storage, config_store, today, args, runtime)
        }
        TaskCommand::Start(args) => start_task(storage, config_store, today, args, runtime),
        TaskCommand::Wait(args) => wait_task(storage, config_store, today, args, runtime),
        TaskCommand::Block(args) => block_task(storage, config_store, today, args, runtime),
        TaskCommand::Add(args) => add_task(storage, config_store, today, args, runtime),
        TaskCommand::List(args) => list_tasks(storage, config_store, today, args, runtime),
        TaskCommand::Ready(args) => ready_tasks(storage, config_store, today, args, runtime),
        TaskCommand::Show(args) => show_task(storage, config_store, today, args, runtime),
        TaskCommand::Edit(args) => edit_task(storage, config_store, today, args, runtime),
        TaskCommand::BulkEdit(args) => bulk_edit_tasks(storage, config_store, today, args, runtime),
        TaskCommand::Done(args) => complete_task(storage, config_store, today, args, runtime),
        TaskCommand::Reopen(args) => reopen_task(storage, config_store, today, args, runtime),
        TaskCommand::Defer(args) => defer_task(storage, config_store, today, args, runtime),
        TaskCommand::Archive(args) => archive_task(storage, config_store, today, args, runtime),
        TaskCommand::Unarchive(args) => unarchive_task(storage, config_store, today, args, runtime),
        TaskCommand::Delete(args) => delete_task(storage, config_store, args, runtime),
    }
}

fn execute_project_command<S: Storage>(
    command: ProjectCommand,
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    runtime: RuntimeOptions,
) -> Result<String> {
    match command {
        ProjectCommand::Add(args) => add_project(storage, config_store, today, args, runtime),
        ProjectCommand::List(args) => list_projects(storage, config_store, today, args, runtime),
        ProjectCommand::Show(args) => show_project(storage, config_store, today, args, runtime),
        ProjectCommand::Edit(args) => edit_project(storage, config_store, today, args, runtime),
        ProjectCommand::Archive(args) => {
            archive_project(storage, config_store, today, args, runtime)
        }
        ProjectCommand::Unarchive(args) => {
            unarchive_project(storage, config_store, today, args, runtime)
        }
    }
}

fn execute_import_command<S: Storage>(
    command: ImportCommand,
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    runtime: RuntimeOptions,
) -> Result<String> {
    match command {
        ImportCommand::Legacy(args) => import_legacy(storage, config_store, today, args, runtime),
    }
}

fn execute_storage_command<S: Storage>(
    command: StorageCommand,
    storage: &S,
    config_store: &JsonConfigStore,
    runtime: RuntimeOptions,
) -> Result<String> {
    match command {
        StorageCommand::Path(args) => show_storage_paths(storage, config_store, args, runtime),
        StorageCommand::Export(args) => export_storage(storage, config_store, args, runtime),
        StorageCommand::Backup(args) => backup_storage(storage, config_store, args, runtime),
    }
}

fn render_completions(args: CompletionsArgs) -> Result<String> {
    let mut command = Cli::command();
    let mut buffer = Cursor::new(Vec::new());

    match args.shell {
        ShellKind::Bash => generate(shells::Bash, &mut command, "kelp", &mut buffer),
        ShellKind::Zsh => generate(shells::Zsh, &mut command, "kelp", &mut buffer),
        ShellKind::Fish => generate(shells::Fish, &mut command, "kelp", &mut buffer),
    }

    String::from_utf8(buffer.into_inner()).map_err(Into::into)
}

fn show_config(
    config_store: &JsonConfigStore,
    _args: ConfigShowArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = config_store.load()?;
    if runtime.wants_json(&config) {
        return to_pretty_json("config.show", &config_response(&config, config_store));
    }

    let response = config_response(&config, config_store);
    Ok(render_confirmation(
        runtime.render,
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

fn set_config(
    config_store: &JsonConfigStore,
    args: ConfigSetArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let mut config = config_store.load()?;
    let mut changed = Vec::new();

    if let Some(upcoming_days) = args.upcoming_days {
        if upcoming_days < 1 {
            return Err(usage_error(
                "invalid_upcoming_days",
                "--upcoming-days must be at least 1",
            ));
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
        return Err(usage_error(
            "missing_config_changes",
            "no config changes were provided",
        ));
    }

    config_store.save(&config)?;

    if runtime.wants_json(&config) {
        return to_pretty_json("config.set", &config_response(&config, config_store));
    }

    Ok(render_confirmation(
        runtime.render,
        "Config updated",
        &changed.join("\n"),
    ))
}

fn execute_review_command<S: Storage>(
    command: crate::cli::ReviewCommand,
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    runtime: RuntimeOptions,
) -> Result<String> {
    match command {
        crate::cli::ReviewCommand::Daily(args) => {
            daily_review(storage, config_store, today, args, runtime)
        }
        crate::cli::ReviewCommand::Weekly(args) => {
            weekly_review(storage, config_store, today, args, runtime)
        }
    }
}

fn add_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskAddArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    let project_id = resolve_optional_project_id(&state, args.project.as_deref())?;
    let due_date = args
        .due
        .as_deref()
        .map(|value| resolve_date_expression(today, value))
        .transpose()?;
    let waiting_until = args
        .wait_until
        .as_deref()
        .map(|value| resolve_date_expression(today, value))
        .transpose()?;
    let task = state.create_task(
        NewTask {
            title: args.title,
            notes: read_optional_text_input(args.notes, args.notes_file.as_deref())?,
            project_id,
            priority: args.priority,
            tags: normalize_tags(args.tags),
            due_date,
            recurrence: args.repeat,
            waiting_until,
            blocked_reason: args.blocked_reason,
            depends_on: parse_task_dependencies(&args.depends_on),
        },
        today,
    )?;
    storage.save(&state)?;

    if runtime.wants_json(&config) {
        return to_pretty_json("task.add", &task_view(&task, &state));
    }

    Ok(render_confirmation(
        runtime.render,
        "Task created",
        &render_task_detail(runtime.render, &task, &state),
    ))
}

fn import_legacy<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: LegacyImportArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    let summary = import_legacy_from_path(&mut state, &args.source, today)?;
    storage.save(&state)?;

    if runtime.wants_json(&config) {
        return to_pretty_json(
            "import.legacy",
            &ImportResponse {
                imported_tasks: summary.imported_tasks,
                imported_projects: summary.imported_projects,
                reused_projects: summary.reused_projects,
                skipped_duplicates: summary.skipped_duplicates,
                scanned_files: summary.scanned_files,
                warnings: summary.warnings,
            },
        );
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
        lines.extend(
            summary
                .warnings
                .into_iter()
                .map(|warning| format!("  - {warning}")),
        );
    }

    Ok(render_confirmation(
        runtime.render,
        "Legacy import complete",
        &lines.join("\n"),
    ))
}

fn show_storage_paths<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    _args: StoragePathArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let info = StorageInfoResponse {
        backend: "json",
        root_dir: storage.root_dir().display().to_string(),
        data_file: storage.data_file().display().to_string(),
        backup_dir: storage.backup_dir().display().to_string(),
        lock_file: storage.lock_file().display().to_string(),
    };

    if runtime.wants_json(&config) {
        return to_pretty_json("storage.path", &info);
    }

    Ok(render_confirmation(
        runtime.render,
        "Storage paths",
        &format!(
            "backend: {}\nroot: {}\ndata: {}\nbackups: {}\nlock: {}",
            info.backend, info.root_dir, info.data_file, info.backup_dir, info.lock_file
        ),
    ))
}

fn export_storage<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    args: StorageExportArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let output = storage.export_to(&args.file)?;
    if runtime.wants_json(&config) {
        return to_pretty_json(
            "storage.export",
            &StoragePathResult {
                path: output.display().to_string(),
            },
        );
    }

    Ok(render_confirmation(
        runtime.render,
        "Storage exported",
        &format!("wrote {}", output.display()),
    ))
}

fn backup_storage<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    _args: StorageBackupArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let backup = storage.create_backup_snapshot()?;
    if runtime.wants_json(&config) {
        return to_pretty_json(
            "storage.backup",
            &StoragePathResult {
                path: backup.display().to_string(),
            },
        );
    }

    Ok(render_confirmation(
        runtime.render,
        "Backup created",
        &format!("wrote {}", backup.display()),
    ))
}

fn start_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskStartArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::InProgress, today)?;
    storage.save(&state)?;

    let task = state.find_task(TaskId(args.id)).ok_or_else(|| {
        not_found_error("task_not_found", format!("task {} does not exist", args.id))
    })?;
    if runtime.wants_json(&config) {
        return to_pretty_json("task.start", &task_view(task, &state));
    }
    Ok(render_confirmation(
        runtime.render,
        "Task started",
        &render_task_detail(runtime.render, task, &state),
    ))
}

fn mark_next_action_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskNextArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::NextAction, today)?;
    storage.save(&state)?;

    let task = state.find_task(TaskId(args.id)).ok_or_else(|| {
        not_found_error("task_not_found", format!("task {} does not exist", args.id))
    })?;
    if runtime.wants_json(&config) {
        return to_pretty_json("task.next", &task_view(task, &state));
    }
    Ok(render_confirmation(
        runtime.render,
        "Task marked as next action",
        &render_task_detail(runtime.render, task, &state),
    ))
}

fn wait_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskWaitArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::Waiting, today)?;
    if let Some(until) = args.until.as_deref() {
        let waiting_until = resolve_date_expression(today, until)?;
        state.apply_task_patch(
            TaskId(args.id),
            TaskPatch {
                waiting_until: Some(Some(waiting_until)),
                ..TaskPatch::default()
            },
            today,
        )?;
    }
    storage.save(&state)?;

    let task = state.find_task(TaskId(args.id)).ok_or_else(|| {
        not_found_error("task_not_found", format!("task {} does not exist", args.id))
    })?;
    if runtime.wants_json(&config) {
        return to_pretty_json("task.wait", &task_view(task, &state));
    }
    Ok(render_confirmation(
        runtime.render,
        "Task marked as waiting",
        &render_task_detail(runtime.render, task, &state),
    ))
}

fn block_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskBlockArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::Blocked, today)?;
    if let Some(reason) = args.reason {
        state.apply_task_patch(
            TaskId(args.id),
            TaskPatch {
                blocked_reason: Some(Some(reason)),
                ..TaskPatch::default()
            },
            today,
        )?;
    }
    storage.save(&state)?;

    let task = state.find_task(TaskId(args.id)).ok_or_else(|| {
        not_found_error("task_not_found", format!("task {} does not exist", args.id))
    })?;
    if runtime.wants_json(&config) {
        return to_pretty_json("task.block", &task_view(task, &state));
    }
    Ok(render_confirmation(
        runtime.render,
        "Task marked as blocked",
        &render_task_detail(runtime.render, task, &state),
    ))
}

fn list_tasks<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskListArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let state = storage.load()?;
    let project_id = resolve_optional_project_id(&state, args.project.as_deref())?;
    let mut tasks = filtered_tasks(
        &state,
        today,
        TaskFilter {
            project_id,
            status: args.status,
            priority: args.priority,
            tags: args.tags.iter().map(String::as_str).collect(),
            query: args.query.as_deref(),
            due_today: args.due_today,
            overdue: args.overdue,
            include_all_statuses: args.all,
            include_archived_projects: args.all,
            ready_only: args.ready,
        },
    );
    sort_tasks(&mut tasks, args.sort.unwrap_or(config.default_task_sort));
    apply_limit(&mut tasks, args.limit);

    if runtime.wants_json(&config) {
        return to_pretty_json(
            "task.list",
            &TaskListResponse {
                tasks: tasks
                    .into_iter()
                    .map(|task| task_view(task, &state))
                    .collect(),
            },
        );
    }

    Ok(render_task_list(runtime.render, "Tasks", &tasks, &state))
}

fn ready_tasks<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskReadyArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let state = storage.load()?;
    let mut tasks = filtered_tasks(
        &state,
        today,
        TaskFilter {
            project_id: None,
            status: None,
            priority: None,
            tags: Vec::new(),
            query: None,
            due_today: false,
            overdue: false,
            include_all_statuses: false,
            include_archived_projects: false,
            ready_only: true,
        },
    );
    sort_ready_tasks(&mut tasks);
    apply_limit(&mut tasks, args.limit);

    if runtime.wants_json(&config) {
        return to_pretty_json(
            "task.ready",
            &TaskListResponse {
                tasks: tasks.iter().map(|task| task_view(task, &state)).collect(),
            },
        );
    }

    Ok(render_task_list(
        runtime.render,
        "Ready tasks",
        &tasks,
        &state,
    ))
}

fn show_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    _today: NaiveDate,
    args: TaskShowArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let state = storage.load()?;
    let task = state.find_task(TaskId(args.id)).ok_or_else(|| {
        not_found_error("task_not_found", format!("task {} does not exist", args.id))
    })?;

    if runtime.wants_json(&config) {
        return to_pretty_json("task.show", &task_view(task, &state));
    }

    Ok(render_task_detail(runtime.render, task, &state))
}

fn edit_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskEditArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    let task_id = TaskId(args.id);
    let patch = build_task_patch(&state, &args, today)?;
    let desired_status = args.status;
    if patch.is_empty() && desired_status.is_none() {
        return Err(usage_error(
            "missing_task_changes",
            "no task changes were provided",
        ));
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

    let task = state.find_task(task_id).ok_or_else(|| {
        not_found_error("task_not_found", format!("task {} does not exist", args.id))
    })?;
    if runtime.wants_json(&config) {
        return to_pretty_json(
            "task.edit",
            &TaskMutationResponse {
                task: task_view(task, &state),
                spawned_task_id: spawned_task_id.map(|task_id| task_id.0),
            },
        );
    }
    let mut output = render_confirmation(
        runtime.render,
        "Task updated",
        &render_task_detail(runtime.render, task, &state),
    );
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
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskBulkEditArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    if args.ids.is_empty() {
        return Err(usage_error(
            "missing_task_ids",
            "bulk edit requires at least one task id",
        ));
    }

    let mut state = storage.load()?;
    let patch = build_bulk_task_patch(&state, &args, today)?;
    let desired_status = args.status;
    if patch.is_empty() && desired_status.is_none() {
        return Err(usage_error(
            "missing_bulk_task_changes",
            "no bulk edit changes were provided",
        ));
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

    if runtime.wants_json(&config) {
        return to_pretty_json(
            "task.bulk_edit",
            &BulkTaskMutationResponse {
                updated_tasks: updated,
                spawned_task_ids: spawned_tasks,
            },
        );
    }

    Ok(render_confirmation(
        runtime.render,
        "Bulk edit applied",
        &format!(
            "updated tasks: {}\nspawned recurring tasks: {}",
            updated,
            format_u64_list(&spawned_tasks)
        ),
    ))
}

fn complete_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskDoneArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    let spawned_task_id = state.complete_task(TaskId(args.id), today)?;
    storage.save(&state)?;

    let task = state.find_task(TaskId(args.id)).ok_or_else(|| {
        not_found_error("task_not_found", format!("task {} does not exist", args.id))
    })?;
    if runtime.wants_json(&config) {
        return to_pretty_json(
            "task.done",
            &TaskMutationResponse {
                task: task_view(task, &state),
                spawned_task_id: spawned_task_id.map(|task_id| task_id.0),
            },
        );
    }
    let mut output = render_confirmation(
        runtime.render,
        "Task completed",
        &render_task_detail(runtime.render, task, &state),
    );
    if let Some(next_task_id) = spawned_task_id {
        output.push_str(&format!(
            "\n{}\nspawned recurring task {}",
            render_separator(),
            next_task_id.0
        ));
    }

    Ok(output)
}

fn reopen_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskReopenArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::Todo, today)?;
    storage.save(&state)?;

    let task = state.find_task(TaskId(args.id)).ok_or_else(|| {
        not_found_error("task_not_found", format!("task {} does not exist", args.id))
    })?;
    if runtime.wants_json(&config) {
        return to_pretty_json("task.reopen", &task_view(task, &state));
    }
    Ok(render_confirmation(
        runtime.render,
        "Task reopened",
        &render_task_detail(runtime.render, task, &state),
    ))
}

fn defer_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskDeferArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
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

    let task = state.find_task(TaskId(args.id)).ok_or_else(|| {
        not_found_error("task_not_found", format!("task {} does not exist", args.id))
    })?;
    if runtime.wants_json(&config) {
        return to_pretty_json("task.defer", &task_view(task, &state));
    }
    Ok(render_confirmation(
        runtime.render,
        "Task deferred",
        &render_task_detail(runtime.render, task, &state),
    ))
}

fn archive_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskArchiveArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::Archived, today)?;
    storage.save(&state)?;

    let task = state.find_task(TaskId(args.id)).ok_or_else(|| {
        not_found_error("task_not_found", format!("task {} does not exist", args.id))
    })?;
    if runtime.wants_json(&config) {
        return to_pretty_json("task.archive", &task_view(task, &state));
    }
    Ok(render_confirmation(
        runtime.render,
        "Task archived",
        &render_task_detail(runtime.render, task, &state),
    ))
}

fn unarchive_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: TaskUnarchiveArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    state.set_task_status(TaskId(args.id), TaskStatus::Todo, today)?;
    storage.save(&state)?;

    let task = state.find_task(TaskId(args.id)).ok_or_else(|| {
        not_found_error("task_not_found", format!("task {} does not exist", args.id))
    })?;
    if runtime.wants_json(&config) {
        return to_pretty_json("task.unarchive", &task_view(task, &state));
    }
    Ok(render_confirmation(
        runtime.render,
        "Task unarchived",
        &render_task_detail(runtime.render, task, &state),
    ))
}

fn delete_task<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    args: TaskDeleteArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    let task = state.delete_task(TaskId(args.id))?;
    storage.save(&state)?;

    if runtime.wants_json(&config) {
        return to_pretty_json(
            "task.delete",
            &DeleteResponse {
                removed_id: task.id.0,
                removed_title: task.title.clone(),
            },
        );
    }

    Ok(render_confirmation(
        runtime.render,
        "Task deleted",
        &format!("removed task {}: {}", task.id.0, task.title),
    ))
}

fn add_project<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: ProjectAddArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    let deadline = args
        .deadline
        .as_deref()
        .map(|value| resolve_date_expression(today, value))
        .transpose()?;
    let project = state.create_project(
        args.name,
        read_optional_text_input(args.description, args.description_file.as_deref())?,
        deadline,
        today,
    )?;
    storage.save(&state)?;

    let summary = state.project_summary(project.id, today)?;
    if runtime.wants_json(&config) {
        return to_pretty_json("project.add", &project_view(&project, summary));
    }

    Ok(render_confirmation(
        runtime.render,
        "Project created",
        &format!("created project {}: {}", project.id.0, project.name),
    ))
}

fn edit_project<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: ProjectEditArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    let project_id = state.resolve_project_id(&args.project)?;
    let patch = build_project_patch(&args, today)?;
    if patch.is_empty() {
        return Err(usage_error(
            "missing_project_changes",
            "no project changes were provided",
        ));
    }

    state.apply_project_patch(project_id, patch, today)?;
    storage.save(&state)?;

    let project = state.find_project(project_id).ok_or_else(|| {
        not_found_error(
            "project_not_found",
            format!("project {} does not exist", args.project),
        )
    })?;
    let summary = state.project_summary(project.id, today)?;
    if runtime.wants_json(&config) {
        return to_pretty_json("project.edit", &project_view(project, summary));
    }
    Ok(render_confirmation(
        runtime.render,
        "Project updated",
        &format!(
            "project {}: {}\ndeadline: {}\ndescription: {}",
            project.id.0,
            project.name,
            project
                .deadline
                .map(|date| date.to_string())
                .unwrap_or_else(|| "none".to_string()),
            project.description.as_deref().unwrap_or("none")
        ),
    ))
}

fn list_projects<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: ProjectListArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
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

    let mut project_entries: Vec<(&Project, ProjectSummary)> = projects
        .iter()
        .map(|project| {
            let summary = state.project_summary(project.id, today)?;
            Ok((*project, summary))
        })
        .collect::<Result<_>>()?;

    if !args.archived {
        if args.at_risk {
            project_entries.retain(|(_, summary)| {
                summary.overdue_tasks > 0
                    || summary.blocked_tasks > 0
                    || summary.dependency_blocked_tasks > 0
                    || (summary.open_tasks > 0 && summary.next_action_tasks == 0)
            });
        }
        if args.missing_next_action {
            project_entries
                .retain(|(_, summary)| summary.open_tasks > 0 && summary.next_action_tasks == 0);
        }
        if let Some(days) = args.deadline_within {
            if days < 1 {
                return Err(usage_error(
                    "invalid_deadline_window",
                    "--deadline-within must be at least 1",
                ));
            }
            let window_end = today + Duration::days(days);
            project_entries.retain(|(project, _)| {
                project
                    .deadline
                    .map(|deadline| deadline >= today && deadline <= window_end)
                    .unwrap_or(false)
            });
        }
    }
    apply_limit(&mut project_entries, args.limit);

    if runtime.wants_json(&config) {
        return to_pretty_json(
            "project.list",
            &ProjectListResponse {
                projects: project_entries
                    .iter()
                    .map(|(project, summary)| project_view(project, *summary))
                    .collect(),
            },
        );
    }

    Ok(render_project_list(
        runtime.render,
        "Projects",
        &project_entries,
    ))
}

fn show_project<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: ProjectShowArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let state = storage.load()?;
    let project_id = state.resolve_project_id(&args.project)?;
    let project = state.find_project(project_id).ok_or_else(|| {
        not_found_error(
            "project_not_found",
            format!("project {} does not exist", args.project),
        )
    })?;
    let summary = state.project_summary(project.id, today)?;
    let mut tasks = state.project_tasks(project.id);
    tasks.retain(|task| !matches!(task.status, TaskStatus::Archived));
    sort_tasks(&mut tasks, TaskSortKey::Due);

    if runtime.wants_json(&config) {
        return to_pretty_json(
            "project.show",
            &ProjectDetailResponse {
                project: project_view(project, summary),
                tasks: tasks
                    .into_iter()
                    .map(|task| task_view(task, &state))
                    .collect(),
            },
        );
    }

    Ok(render_project_detail(
        runtime.render,
        project,
        summary,
        &tasks,
        &state,
    ))
}

fn archive_project<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: ProjectArchiveArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    let project_id = state.resolve_project_id(&args.project)?;
    state.archive_project(project_id, today)?;
    storage.save(&state)?;

    let project = state.find_project(project_id).ok_or_else(|| {
        not_found_error(
            "project_not_found",
            format!("project {} does not exist", args.project),
        )
    })?;
    let summary = state.project_summary(project.id, today)?;
    if runtime.wants_json(&config) {
        return to_pretty_json("project.archive", &project_view(project, summary));
    }
    Ok(render_confirmation(
        runtime.render,
        "Project archived",
        &format!("archived project {}: {}", project.id.0, project.name),
    ))
}

fn unarchive_project<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: ProjectUnarchiveArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let mut state = storage.load()?;
    let project_id = state.resolve_project_id(&args.project)?;
    state.activate_project(project_id, today)?;
    storage.save(&state)?;

    let project = state.find_project(project_id).ok_or_else(|| {
        not_found_error(
            "project_not_found",
            format!("project {} does not exist", args.project),
        )
    })?;
    let summary = state.project_summary(project.id, today)?;
    if runtime.wants_json(&config) {
        return to_pretty_json("project.unarchive", &project_view(project, summary));
    }
    Ok(render_confirmation(
        runtime.render,
        "Project reactivated",
        &format!("reactivated project {}: {}", project.id.0, project.name),
    ))
}

fn execute_today<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    _args: crate::cli::ListOutputArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
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
    let mut dependency_blocked = active_open_tasks(&state)
        .into_iter()
        .filter(|task| state.has_unresolved_dependencies(task))
        .collect::<Vec<_>>();
    let mut waiting_follow_up = active_open_tasks(&state)
        .into_iter()
        .filter(|task| {
            matches!(task.status, TaskStatus::Waiting)
                && task
                    .waiting_until
                    .map(|until| until <= today)
                    .unwrap_or(true)
        })
        .collect::<Vec<_>>();

    sort_tasks(&mut overdue, TaskSortKey::Due);
    sort_tasks(&mut due_today, TaskSortKey::Due);
    sort_tasks(&mut next_actions, TaskSortKey::Priority);
    sort_tasks(&mut in_progress, TaskSortKey::Due);
    sort_tasks(&mut blocked, TaskSortKey::Priority);
    sort_tasks(&mut dependency_blocked, TaskSortKey::Priority);
    sort_tasks(&mut waiting_follow_up, TaskSortKey::Due);

    let sections = vec![
        ("Overdue".to_string(), overdue),
        ("Due today".to_string(), due_today),
        ("Next actions".to_string(), next_actions),
        ("In progress".to_string(), in_progress),
        ("Blocked".to_string(), blocked),
        ("Blocked by dependencies".to_string(), dependency_blocked),
        ("Waiting follow-up".to_string(), waiting_follow_up),
    ];

    if runtime.wants_json(&config) {
        return to_pretty_json(
            "today",
            &SectionedTaskResponse {
                sections: sections_to_views(&sections, &state),
            },
        );
    }

    Ok(render_task_sections(
        runtime.render,
        "Today",
        &sections,
        &state,
    ))
}

fn execute_upcoming<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: UpcomingArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let days = args.days.unwrap_or(config.default_upcoming_days);
    if days < 1 {
        return Err(usage_error(
            "invalid_upcoming_days",
            "--days must be at least 1",
        ));
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
    if runtime.wants_json(&config) {
        return to_pretty_json(
            "upcoming",
            &SectionedTaskResponse {
                sections: sections_to_views(&sections, &state),
            },
        );
    }

    Ok(render_task_sections(
        runtime.render,
        "Upcoming",
        &sections,
        &state,
    ))
}

fn daily_review<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: ReviewArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
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
    let mut dependency_blocked = active_open_tasks(&state)
        .into_iter()
        .filter(|task| state.has_unresolved_dependencies(task))
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
    sort_tasks(&mut dependency_blocked, TaskSortKey::Priority);
    sort_tasks(&mut needs_scheduling, TaskSortKey::Due);

    let sections = vec![
        ("Carryover".to_string(), carryover),
        ("Due today".to_string(), due_today),
        ("Next actions".to_string(), next_actions),
        ("Blocked".to_string(), blocked),
        ("Waiting".to_string(), waiting),
        ("Blocked by dependencies".to_string(), dependency_blocked),
        ("Needs scheduling".to_string(), needs_scheduling),
    ];

    if runtime.wants_json(&config) {
        return to_pretty_json(
            "review.daily",
            &ReviewTaskResponse {
                applied_actions,
                sections: sections_to_views(&sections, &state),
            },
        );
    }

    Ok(render_review_output(
        runtime.render,
        "Daily review",
        &applied_actions,
        render_task_sections(runtime.render, "Daily review", &sections, &state),
    ))
}

fn weekly_review<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: ReviewArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
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
    let mut waiting_follow_up = active_open_tasks(&state)
        .into_iter()
        .filter(|task| {
            matches!(task.status, TaskStatus::Waiting)
                && task
                    .waiting_until
                    .map(|until| until <= today)
                    .unwrap_or(true)
        })
        .collect::<Vec<_>>();
    let mut dependency_blocked = active_open_tasks(&state)
        .into_iter()
        .filter(|task| state.has_unresolved_dependencies(task))
        .collect::<Vec<_>>();
    let mut stale_tasks = active_open_tasks(&state)
        .into_iter()
        .filter(|task| task.updated_on <= stale_cutoff)
        .collect::<Vec<_>>();
    let mut projects_without_next_actions = active_projects(&state)
        .into_iter()
        .filter_map(|project| {
            let summary = state.project_summary(project.id, today).ok()?;
            if summary.open_tasks > 0 && summary.next_action_tasks == 0 {
                Some((project, summary))
            } else {
                None
            }
        })
        .collect::<Vec<_>>();
    let mut stalled_projects = active_projects(&state)
        .into_iter()
        .filter_map(|project| {
            let summary = state.project_summary(project.id, today).ok()?;
            if summary.open_tasks > 0
                && (summary.blocked_tasks > 0 || summary.dependency_blocked_tasks > 0)
            {
                Some((project, summary))
            } else {
                None
            }
        })
        .collect::<Vec<_>>();
    let mut projects_missing_deadlines = active_projects(&state)
        .into_iter()
        .filter_map(|project| {
            if project.deadline.is_none() {
                let summary = state.project_summary(project.id, today).ok()?;
                Some((project, summary))
            } else {
                None
            }
        })
        .collect::<Vec<_>>();
    let mut deadline_projects = active_projects(&state)
        .into_iter()
        .filter_map(|project| {
            let deadline = project.deadline?;
            if deadline >= today && deadline <= window_end {
                let summary = state.project_summary(project.id, today).ok()?;
                Some((project, summary))
            } else {
                None
            }
        })
        .collect::<Vec<_>>();
    let mut at_risk_projects = active_projects(&state)
        .into_iter()
        .filter_map(|project| {
            let deadline = project.deadline?;
            let summary = state.project_summary(project.id, today).ok()?;
            if deadline >= today
                && deadline <= window_end
                && (summary.overdue_tasks > 0
                    || summary.blocked_tasks > 0
                    || summary.dependency_blocked_tasks > 0
                    || summary.next_action_tasks == 0)
            {
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
    sort_tasks(&mut waiting_follow_up, TaskSortKey::Due);
    sort_tasks(&mut dependency_blocked, TaskSortKey::Priority);
    sort_tasks(&mut due_this_week, TaskSortKey::Due);
    sort_tasks(&mut stale_tasks, TaskSortKey::Updated);
    projects_without_next_actions
        .sort_by(|left, right| left.0.name.to_lowercase().cmp(&right.0.name.to_lowercase()));
    stalled_projects
        .sort_by(|left, right| left.0.name.to_lowercase().cmp(&right.0.name.to_lowercase()));
    projects_missing_deadlines
        .sort_by(|left, right| left.0.name.to_lowercase().cmp(&right.0.name.to_lowercase()));
    deadline_projects.sort_by(|left, right| {
        left.0
            .deadline
            .cmp(&right.0.deadline)
            .then_with(|| left.0.name.to_lowercase().cmp(&right.0.name.to_lowercase()))
    });
    at_risk_projects.sort_by(|left, right| {
        left.0
            .deadline
            .cmp(&right.0.deadline)
            .then_with(|| left.0.name.to_lowercase().cmp(&right.0.name.to_lowercase()))
    });

    let sections = vec![
        ("Overdue".to_string(), overdue),
        ("Next actions".to_string(), next_actions),
        ("Blocked".to_string(), blocked),
        ("Waiting".to_string(), waiting),
        ("Waiting follow-up".to_string(), waiting_follow_up),
        ("Blocked by dependencies".to_string(), dependency_blocked),
        ("Due this week".to_string(), due_this_week),
        ("Stale tasks".to_string(), stale_tasks),
    ];

    if runtime.wants_json(&config) {
        return to_pretty_json(
            "review.weekly",
            &WeeklyReviewResponse {
                applied_actions,
                sections: sections_to_views(&sections, &state),
                projects_without_next_actions: projects_without_next_actions
                    .iter()
                    .map(|(project, summary)| project_view(project, *summary))
                    .collect(),
                stalled_projects: stalled_projects
                    .iter()
                    .map(|(project, summary)| project_view(project, *summary))
                    .collect(),
                projects_missing_deadlines: projects_missing_deadlines
                    .iter()
                    .map(|(project, summary)| project_view(project, *summary))
                    .collect(),
                deadline_projects: deadline_projects
                    .iter()
                    .map(|(project, summary)| project_view(project, *summary))
                    .collect(),
                at_risk_projects: at_risk_projects
                    .iter()
                    .map(|(project, summary)| project_view(project, *summary))
                    .collect(),
            },
        );
    }

    let mut output = render_task_sections(runtime.render, "Weekly review", &sections, &state);
    output.push_str("\n\n");
    output.push_str(&render_project_list(
        runtime.render,
        "Projects without next actions",
        &projects_without_next_actions,
    ));
    output.push_str("\n\n");
    output.push_str(&render_project_list(
        runtime.render,
        "Projects stalled by blockers",
        &stalled_projects,
    ));
    output.push_str("\n\n");
    output.push_str(&render_project_list(
        runtime.render,
        "Projects missing deadlines",
        &projects_missing_deadlines,
    ));
    output.push_str("\n\n");
    output.push_str(&render_project_list(
        runtime.render,
        "Projects due this week",
        &deadline_projects,
    ));
    output.push_str("\n\n");
    output.push_str(&render_project_list(
        runtime.render,
        "Projects at risk this week",
        &at_risk_projects,
    ));
    Ok(render_review_output(
        runtime.render,
        "Weekly review",
        &applied_actions,
        output,
    ))
}

fn execute_search<S: Storage>(
    storage: &S,
    config_store: &JsonConfigStore,
    today: NaiveDate,
    args: SearchArgs,
    runtime: RuntimeOptions,
) -> Result<String> {
    let config = load_config(config_store)?;
    let state = storage.load()?;
    let query = args.query.trim();
    if query.is_empty() {
        return Err(usage_error(
            "empty_search_query",
            "search query cannot be empty",
        ));
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
                        .map(|project_name| {
                            project_name.to_lowercase().contains(&query.to_lowercase())
                        })
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

    if runtime.wants_json(&config) {
        return to_pretty_json(
            "search",
            &SearchResponse {
                tasks: tasks.iter().map(|task| task_view(task, &state)).collect(),
                projects: projects
                    .iter()
                    .map(|(project, summary)| project_view(project, *summary))
                    .collect(),
            },
        );
    }

    Ok(render_search_results(
        runtime.render,
        &tasks,
        &projects,
        &state,
    ))
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
        read_optional_text_input(args.notes.clone(), args.notes_file.as_deref())?.map(Some)
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

    let waiting_until = if args.clear_wait_until {
        Some(None)
    } else {
        args.wait_until
            .as_deref()
            .map(|value| resolve_date_expression(today, value))
            .transpose()?
            .map(Some)
    };

    let blocked_reason = if args.clear_blocked_reason {
        Some(None)
    } else {
        args.blocked_reason.clone().map(Some)
    };

    let depends_on = if args.clear_depends_on {
        Some(Vec::new())
    } else if args.depends_on.is_empty() {
        None
    } else {
        Some(parse_task_dependencies(&args.depends_on))
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
        waiting_until,
        blocked_reason,
        depends_on,
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
        waiting_until: None,
        blocked_reason: None,
        depends_on: None,
    })
}

fn parse_task_dependencies(values: &[u64]) -> Vec<TaskId> {
    values.iter().copied().map(TaskId).collect()
}

fn read_optional_text_input(inline: Option<String>, file: Option<&Path>) -> Result<Option<String>> {
    if let Some(path) = file {
        let contents = fs::read_to_string(path)
            .with_context(|| format!("failed to read {}", path.display()))?;
        return Ok(
            Some(contents.trim_end_matches(&['\n', '\r'][..]).to_string())
                .filter(|contents| !contents.trim().is_empty()),
        );
    }

    Ok(inline.filter(|value| !value.trim().is_empty()))
}

fn resolve_optional_project_id(
    state: &AppState,
    project_ref: Option<&str>,
) -> Result<Option<ProjectId>> {
    project_ref
        .map(|reference| state.resolve_project_id(reference))
        .transpose()
        .map_err(Into::into)
}

fn build_project_patch(args: &ProjectEditArgs, today: NaiveDate) -> Result<ProjectPatch> {
    let description = if args.clear_description {
        Some(None)
    } else {
        read_optional_text_input(args.description.clone(), args.description_file.as_deref())?
            .map(Some)
    };

    let deadline = if args.clear_deadline {
        Some(None)
    } else {
        args.deadline
            .as_deref()
            .map(|value| resolve_date_expression(today, value))
            .transpose()?
            .map(Some)
    };

    Ok(ProjectPatch {
        description,
        deadline,
    })
}

fn resolve_defer_date(today: NaiveDate, args: &TaskDeferArgs) -> Result<NaiveDate> {
    match (&args.until, args.days) {
        (Some(due_date), None) => resolve_date_expression(today, due_date),
        (None, Some(days)) if days > 0 => Ok(today + Duration::days(days)),
        (None, Some(_)) => Err(usage_error(
            "invalid_defer_days",
            "--days must be greater than 0",
        )),
        (None, None) => Err(usage_error(
            "missing_defer_date",
            "provide either --until YYYY-MM-DD or --days N",
        )),
        (Some(_), Some(_)) => Err(usage_error(
            "conflicting_defer_options",
            "--until and --days cannot be used together",
        )),
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
            let project = state.find_project(project_id).ok_or_else(|| {
                not_found_error(
                    "project_not_found",
                    format!("project {project_ref} does not exist"),
                )
            })?;
            if matches!(project.status, ProjectStatus::Archived) {
                return Err(conflict_error(
                    "project_archived",
                    format!(
                        "cannot plan next actions in archived project '{}'",
                        project.name
                    ),
                ));
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
                waiting_until: None,
                blocked_reason: None,
                depends_on: Vec::new(),
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

fn task_matches_filter(
    task: &Task,
    state: &AppState,
    today: NaiveDate,
    filter: &TaskFilter<'_>,
) -> bool {
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

    if !filter.tags.iter().all(|tag| task.has_tag(tag)) {
        return false;
    }

    if let Some(query) = filter.query {
        if !task.matches_query(query) {
            return false;
        }
    }

    if filter.ready_only && !is_ready_task(task, state) {
        return false;
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

fn is_ready_task(task: &Task, state: &AppState) -> bool {
    task.project_id.is_some()
        && matches!(
            task.status,
            TaskStatus::Todo | TaskStatus::NextAction | TaskStatus::InProgress
        )
        && task_in_active_project(task, state)
        && !state.has_unresolved_dependencies(task)
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

fn sort_ready_tasks(tasks: &mut Vec<&Task>) {
    tasks.sort_by(|left, right| {
        right
            .status
            .is_next_action()
            .cmp(&left.status.is_next_action())
            .then_with(|| compare_due_dates(left, right))
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

fn group_tasks_by_due_date(tasks: Vec<&Task>) -> Vec<(String, Vec<&Task>)> {
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

fn render_review_output(
    render: RenderOptions,
    title: &str,
    applied_actions: &[String],
    body: String,
) -> String {
    if applied_actions.is_empty() {
        return body;
    }

    format!(
        "{}\n{}\n\n{}",
        render_confirmation(
            render,
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

fn apply_limit<T>(items: &mut Vec<T>, limit: Option<usize>) {
    if let Some(limit) = limit {
        items.truncate(limit);
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

fn load_config(config_store: &JsonConfigStore) -> Result<AppConfig> {
    config_store.load()
}

fn resolve_date_expression(today: NaiveDate, value: &str) -> Result<NaiveDate> {
    let normalized = value.trim().to_lowercase().replace('_', "-");
    if normalized.is_empty() {
        return Err(usage_error(
            "empty_date_expression",
            "date expression cannot be empty",
        ));
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
            .ok_or_else(|| usage_error("invalid_date_expression", format!("failed to resolve date expression '{value}'"))),
        _ => NaiveDate::parse_from_str(value.trim(), "%Y-%m-%d")
            .map_err(|_| {
                usage_error(
                    "invalid_date_expression",
                    format!(
                        "invalid date '{value}', expected YYYY-MM-DD or keywords like today, tomorrow, next-week, next-monday, or +3d"
                    ),
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
        return Err(usage_error(
            "invalid_relative_date_expression",
            format!("invalid relative date expression '{value}'"),
        ));
    }

    let parsed_days = days.parse::<i64>().map_err(|_| {
        usage_error(
            "invalid_relative_date_expression",
            format!("invalid relative date expression '{value}'"),
        )
    })?;
    if parsed_days < 0 {
        return Err(usage_error(
            "invalid_relative_date_expression",
            "relative date expressions must be non-negative",
        ));
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
        waiting_until: task.waiting_until,
        blocked_reason: task.blocked_reason.clone(),
        depends_on: task.depends_on.iter().map(|task_id| task_id.0).collect(),
        unresolved_dependencies: state
            .unresolved_task_dependencies(task)
            .into_iter()
            .map(|task_id| task_id.0)
            .collect(),
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
        deadline: project.deadline,
        summary,
    }
}

fn to_pretty_json<T: Serialize>(command: &str, value: &T) -> Result<String> {
    success_json(command, value)
}

struct TaskFilter<'a> {
    project_id: Option<ProjectId>,
    status: Option<TaskStatus>,
    priority: Option<Priority>,
    tags: Vec<&'a str>,
    query: Option<&'a str>,
    due_today: bool,
    overdue: bool,
    include_all_statuses: bool,
    include_archived_projects: bool,
    ready_only: bool,
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
    waiting_until: Option<NaiveDate>,
    blocked_reason: Option<String>,
    depends_on: Vec<u64>,
    unresolved_dependencies: Vec<u64>,
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
    deadline: Option<NaiveDate>,
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
struct TaskMutationResponse {
    task: TaskView,
    spawned_task_id: Option<u64>,
}

#[derive(Debug, Serialize)]
struct BulkTaskMutationResponse {
    updated_tasks: usize,
    spawned_task_ids: Vec<u64>,
}

#[derive(Debug, Serialize)]
struct DeleteResponse {
    removed_id: u64,
    removed_title: String,
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
    projects_without_next_actions: Vec<ProjectView>,
    stalled_projects: Vec<ProjectView>,
    projects_missing_deadlines: Vec<ProjectView>,
    deadline_projects: Vec<ProjectView>,
    at_risk_projects: Vec<ProjectView>,
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
