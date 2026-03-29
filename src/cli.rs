use crate::config::TaskSortKey;
use crate::domain::{Priority, RecurrenceRule, TaskStatus};
use clap::{Args, Parser, Subcommand, ValueEnum};
use std::path::PathBuf;

const DATE_HELP: &str =
    "Date expressions: YYYY-MM-DD, today, tomorrow, next-week, next-month, next-monday, or +3d.";
const REVIEW_DEFER_HELP: &str =
    "Defer a task with ID:DATE. DATE accepts YYYY-MM-DD, today, tomorrow, next-week, next-month, next-monday, or +3d.";

#[derive(Debug, Clone, Copy, PartialEq, Eq, ValueEnum)]
pub enum OutputFormat {
    Plain,
    Json,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, ValueEnum, Default)]
pub enum ColorMode {
    #[default]
    Auto,
    Always,
    Never,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, ValueEnum)]
pub enum ShellKind {
    Bash,
    Zsh,
    Fish,
}

#[derive(Debug, Parser, Clone)]
#[command(
    name = "kelp",
    version,
    about = "A strict, local-first planner CLI for tasks, projects, and reviews.",
    long_about = "Kelp is a local-first planner for explicit task and project workflows. It is designed for shell users and agentic tooling that need stable JSON output, deterministic subcommands, and zero interactive prompts.",
    after_help = "Examples:\n  kelp init\n  kelp --output json task add --title \"Draft release notes\" --project Launch --due next-monday\n  kelp task ready --limit 10\n  kelp review weekly\n  kelp storage export --file ./kelp-export.json"
)]
pub struct Cli {
    #[arg(long, global = true, value_enum)]
    pub output: Option<OutputFormat>,
    #[arg(long, global = true, hide = true, conflicts_with = "output")]
    pub json: bool,
    #[arg(long, global = true, value_enum, default_value_t = ColorMode::Auto)]
    pub color: ColorMode,
    #[arg(long, global = true)]
    pub data_dir: Option<PathBuf>,
    #[command(subcommand)]
    pub command: Command,
}

impl Cli {
    pub fn requested_output(&self) -> Option<OutputFormat> {
        if self.json {
            Some(OutputFormat::Json)
        } else {
            self.output
        }
    }
}

#[derive(Debug, Subcommand, Clone)]
pub enum Command {
    #[command(about = "Initialize Kelp storage in the current data directory.")]
    Init,
    #[command(about = "Inspect or update planner defaults.")]
    Config {
        #[command(subcommand)]
        command: ConfigCommand,
    },
    #[command(about = "Import data from legacy Kelp storage.")]
    Import {
        #[command(subcommand)]
        command: ImportCommand,
    },
    #[command(about = "Inspect storage paths, backups, and exports.")]
    Storage {
        #[command(subcommand)]
        command: StorageCommand,
    },
    #[command(about = "Manage individual tasks.")]
    Task {
        #[command(subcommand)]
        command: TaskCommand,
    },
    #[command(about = "Manage projects and project-level signals.")]
    Project {
        #[command(subcommand)]
        command: ProjectCommand,
    },
    #[command(about = "Show today's actionable planner view.")]
    Today(ListOutputArgs),
    #[command(about = "Show tasks due within the configured upcoming window.")]
    Upcoming(UpcomingArgs),
    #[command(about = "Run daily or weekly reviews.")]
    Review {
        #[command(subcommand)]
        command: ReviewCommand,
    },
    #[command(visible_alias = "find", about = "Search tasks and projects.")]
    Search(SearchArgs),
    #[command(
        visible_alias = "completion",
        about = "Generate shell completion scripts."
    )]
    Completions(CompletionsArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum ConfigCommand {
    #[command(about = "Show the active config file and defaults.")]
    Show(ConfigShowArgs),
    #[command(about = "Set planner defaults for future commands.")]
    Set(ConfigSetArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum ImportCommand {
    #[command(about = "Import from the legacy .kelpStorage and .kelpProjects format.")]
    Legacy(LegacyImportArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum StorageCommand {
    #[command(about = "Show the active storage paths.")]
    Path(StoragePathArgs),
    #[command(about = "Export the full data file to a path.")]
    Export(StorageExportArgs),
    #[command(about = "Create a point-in-time backup snapshot.")]
    Backup(StorageBackupArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum TaskCommand {
    #[command(
        visible_alias = "create",
        about = "Create a task.",
        after_help = "Examples:\n  kelp task add --title \"Draft changelog\"\n  kelp task add --title \"Prep launch\" --project Launch --priority high --due next-monday\n  kelp task add --title \"Review notes\" --notes-file ./notes.md --tag docs --tag launch"
    )]
    Add(TaskAddArgs),
    #[command(
        visible_alias = "ls",
        about = "List tasks with explicit filters.",
        after_help = "Examples:\n  kelp task list --project Launch --status next_action\n  kelp task list --ready --limit 20\n  kelp task list --tag ops --tag urgent --query release"
    )]
    List(TaskListArgs),
    #[command(
        about = "List actionable project tasks only.",
        after_help = "Ready tasks exclude waiting, blocked, archived, and dependency-blocked work, and only include tasks in active projects."
    )]
    Ready(TaskReadyArgs),
    #[command(about = "Show a single task by id.")]
    Show(TaskShowArgs),
    #[command(
        about = "Edit a task without prompts.",
        after_help = "Examples:\n  kelp task edit 12 --due tomorrow --priority high\n  kelp task edit 12 --notes-file ./notes.md\n  kelp task edit 12 --clear-due --clear-tags"
    )]
    Edit(TaskEditArgs),
    #[command(about = "Apply the same task changes to multiple ids.")]
    BulkEdit(TaskBulkEditArgs),
    #[command(visible_alias = "na", about = "Mark a task as the next action.")]
    Next(TaskNextArgs),
    #[command(visible_alias = "begin", about = "Mark a task as in progress.")]
    Start(TaskStartArgs),
    #[command(visible_alias = "hold", about = "Mark a task as waiting.")]
    Wait(TaskWaitArgs),
    #[command(visible_alias = "stuck", about = "Mark a task as blocked.")]
    Block(TaskBlockArgs),
    #[command(visible_alias = "complete", about = "Complete a task.")]
    Done(TaskDoneArgs),
    #[command(about = "Reopen a completed or archived task.")]
    Reopen(TaskReopenArgs),
    #[command(visible_alias = "snooze", about = "Defer a task to a later date.")]
    Defer(TaskDeferArgs),
    #[command(about = "Archive a task without deleting it.")]
    Archive(TaskArchiveArgs),
    #[command(about = "Restore an archived task.")]
    Unarchive(TaskUnarchiveArgs),
    #[command(visible_alias = "rm", about = "Delete a task permanently.")]
    Delete(TaskDeleteArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum ProjectCommand {
    #[command(
        visible_alias = "create",
        about = "Create a project.",
        after_help = "Examples:\n  kelp project add --name Launch\n  kelp project add --name Launch --description-file ./brief.md --deadline next-month"
    )]
    Add(ProjectAddArgs),
    #[command(
        visible_alias = "ls",
        about = "List projects and project-level planner signals.",
        after_help = "Examples:\n  kelp project list --missing-next-action\n  kelp project list --at-risk --deadline-within 7\n  kelp project list --archived"
    )]
    List(ProjectListArgs),
    #[command(about = "Show a project by name or id.")]
    Show(ProjectShowArgs),
    #[command(
        about = "Edit a project without prompts.",
        after_help = "Examples:\n  kelp project edit Launch --deadline next-week\n  kelp project edit Launch --description-file ./brief.md\n  kelp project edit Launch --clear-description"
    )]
    Edit(ProjectEditArgs),
    #[command(about = "Archive a project.")]
    Archive(ProjectArchiveArgs),
    #[command(about = "Restore an archived project.")]
    Unarchive(ProjectUnarchiveArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum ReviewCommand {
    #[command(
        visible_alias = "day",
        about = "Run a daily review and optionally apply task actions.",
        after_help = "Examples:\n  kelp review daily\n  kelp review daily --start 10 --defer 11:tomorrow --complete 12"
    )]
    Daily(ReviewArgs),
    #[command(
        visible_alias = "week",
        about = "Run a weekly review and optionally apply planning actions.",
        after_help = "Examples:\n  kelp review weekly\n  kelp review weekly --next-action 8 --plan Launch:\"Ship launch checklist\"\n  kelp review weekly --archive 22 --defer 21:+3d"
    )]
    Weekly(ReviewArgs),
}

#[derive(Debug, Args, Clone, Default)]
pub struct ListOutputArgs {}

#[derive(Debug, Args, Clone, Default)]
pub struct ConfigShowArgs {}

#[derive(Debug, Args, Clone)]
pub struct ConfigSetArgs {
    #[arg(long)]
    pub upcoming_days: Option<i64>,
    #[arg(long, value_enum)]
    pub task_sort: Option<TaskSortKey>,
    #[arg(long, conflicts_with = "plain_output")]
    pub json_output: bool,
    #[arg(long, conflicts_with = "json_output")]
    pub plain_output: bool,
}

#[derive(Debug, Args, Clone)]
pub struct CompletionsArgs {
    #[arg(value_enum)]
    pub shell: ShellKind,
}

#[derive(Debug, Args, Clone)]
pub struct LegacyImportArgs {
    #[arg(long, default_value = ".")]
    pub source: PathBuf,
}

#[derive(Debug, Args, Clone, Default)]
pub struct StoragePathArgs {}

#[derive(Debug, Args, Clone)]
pub struct StorageExportArgs {
    #[arg(long = "file")]
    pub file: PathBuf,
}

#[derive(Debug, Args, Clone, Default)]
pub struct StorageBackupArgs {}

#[derive(Debug, Args, Clone)]
pub struct TaskAddArgs {
    #[arg(long)]
    pub title: String,
    #[arg(long, conflicts_with = "notes_file")]
    pub notes: Option<String>,
    #[arg(long, conflicts_with = "notes")]
    pub notes_file: Option<PathBuf>,
    #[arg(long)]
    pub project: Option<String>,
    #[arg(long, value_enum, default_value_t = Priority::Medium)]
    pub priority: Priority,
    #[arg(long = "tag")]
    pub tags: Vec<String>,
    #[arg(long, help = DATE_HELP)]
    pub due: Option<String>,
    #[arg(long, value_enum)]
    pub repeat: Option<RecurrenceRule>,
    #[arg(long, help = DATE_HELP)]
    pub wait_until: Option<String>,
    #[arg(long)]
    pub blocked_reason: Option<String>,
    #[arg(long = "depends-on")]
    pub depends_on: Vec<u64>,
}

#[derive(Debug, Args, Clone)]
pub struct TaskListArgs {
    #[arg(long)]
    pub project: Option<String>,
    #[arg(long, value_enum)]
    pub status: Option<TaskStatus>,
    #[arg(long, value_enum)]
    pub priority: Option<Priority>,
    #[arg(long = "tag")]
    pub tags: Vec<String>,
    #[arg(long)]
    pub query: Option<String>,
    #[arg(long)]
    pub due_today: bool,
    #[arg(long)]
    pub overdue: bool,
    #[arg(long)]
    pub all: bool,
    #[arg(long)]
    pub ready: bool,
    #[arg(long, value_enum)]
    pub sort: Option<TaskSortKey>,
    #[arg(long)]
    pub limit: Option<usize>,
}

#[derive(Debug, Args, Clone)]
pub struct TaskReadyArgs {
    #[arg(long)]
    pub limit: Option<usize>,
}

#[derive(Debug, Args, Clone)]
pub struct TaskShowArgs {
    pub id: u64,
}

#[derive(Debug, Args, Clone)]
pub struct TaskEditArgs {
    pub id: u64,
    #[arg(long)]
    pub title: Option<String>,
    #[arg(long, conflicts_with_all = ["clear_notes", "notes_file"])]
    pub notes: Option<String>,
    #[arg(long, conflicts_with_all = ["clear_notes", "notes"])]
    pub notes_file: Option<PathBuf>,
    #[arg(long)]
    pub clear_notes: bool,
    #[arg(long, conflicts_with = "clear_project")]
    pub project: Option<String>,
    #[arg(long)]
    pub clear_project: bool,
    #[arg(long, value_enum)]
    pub status: Option<TaskStatus>,
    #[arg(long, value_enum)]
    pub priority: Option<Priority>,
    #[arg(long = "tag", conflicts_with = "clear_tags")]
    pub tags: Vec<String>,
    #[arg(long)]
    pub clear_tags: bool,
    #[arg(long, conflicts_with = "clear_due", help = DATE_HELP)]
    pub due: Option<String>,
    #[arg(long)]
    pub clear_due: bool,
    #[arg(long, value_enum, conflicts_with = "clear_repeat")]
    pub repeat: Option<RecurrenceRule>,
    #[arg(long)]
    pub clear_repeat: bool,
    #[arg(long, conflicts_with = "clear_wait_until", help = DATE_HELP)]
    pub wait_until: Option<String>,
    #[arg(long)]
    pub clear_wait_until: bool,
    #[arg(long, conflicts_with = "clear_blocked_reason")]
    pub blocked_reason: Option<String>,
    #[arg(long)]
    pub clear_blocked_reason: bool,
    #[arg(long = "depends-on", conflicts_with = "clear_depends_on")]
    pub depends_on: Vec<u64>,
    #[arg(long)]
    pub clear_depends_on: bool,
}

#[derive(Debug, Args, Clone)]
pub struct TaskBulkEditArgs {
    pub ids: Vec<u64>,
    #[arg(long, conflicts_with = "clear_project")]
    pub project: Option<String>,
    #[arg(long)]
    pub clear_project: bool,
    #[arg(long, value_enum)]
    pub status: Option<TaskStatus>,
    #[arg(long, value_enum)]
    pub priority: Option<Priority>,
    #[arg(long = "tag", conflicts_with = "clear_tags")]
    pub tags: Vec<String>,
    #[arg(long)]
    pub clear_tags: bool,
    #[arg(long, conflicts_with = "clear_due", help = DATE_HELP)]
    pub due: Option<String>,
    #[arg(long)]
    pub clear_due: bool,
    #[arg(long, value_enum, conflicts_with = "clear_repeat")]
    pub repeat: Option<RecurrenceRule>,
    #[arg(long)]
    pub clear_repeat: bool,
}

#[derive(Debug, Args, Clone)]
pub struct TaskStartArgs {
    pub id: u64,
}

#[derive(Debug, Args, Clone)]
pub struct TaskNextArgs {
    pub id: u64,
}

#[derive(Debug, Args, Clone)]
pub struct TaskWaitArgs {
    pub id: u64,
    #[arg(long, help = DATE_HELP)]
    pub until: Option<String>,
}

#[derive(Debug, Args, Clone)]
pub struct TaskBlockArgs {
    pub id: u64,
    #[arg(long)]
    pub reason: Option<String>,
}

#[derive(Debug, Args, Clone)]
pub struct TaskDoneArgs {
    pub id: u64,
}

#[derive(Debug, Args, Clone)]
pub struct TaskReopenArgs {
    pub id: u64,
}

#[derive(Debug, Args, Clone)]
pub struct TaskDeferArgs {
    pub id: u64,
    #[arg(long, conflicts_with = "days", help = DATE_HELP)]
    pub until: Option<String>,
    #[arg(long, conflicts_with = "until")]
    pub days: Option<i64>,
}

#[derive(Debug, Args, Clone)]
pub struct TaskArchiveArgs {
    pub id: u64,
}

#[derive(Debug, Args, Clone)]
pub struct TaskUnarchiveArgs {
    pub id: u64,
}

#[derive(Debug, Args, Clone)]
pub struct TaskDeleteArgs {
    pub id: u64,
}

#[derive(Debug, Args, Clone)]
pub struct ProjectAddArgs {
    #[arg(long)]
    pub name: String,
    #[arg(long, conflicts_with = "description_file")]
    pub description: Option<String>,
    #[arg(long, conflicts_with = "description")]
    pub description_file: Option<PathBuf>,
    #[arg(long, help = DATE_HELP)]
    pub deadline: Option<String>,
}

#[derive(Debug, Args, Clone)]
pub struct ProjectListArgs {
    #[arg(long)]
    pub archived: bool,
    #[arg(long)]
    pub at_risk: bool,
    #[arg(long)]
    pub missing_next_action: bool,
    #[arg(long)]
    pub deadline_within: Option<i64>,
    #[arg(long)]
    pub limit: Option<usize>,
}

#[derive(Debug, Args, Clone)]
pub struct ProjectShowArgs {
    pub project: String,
}

#[derive(Debug, Args, Clone)]
pub struct ProjectEditArgs {
    pub project: String,
    #[arg(long, conflicts_with_all = ["clear_description", "description_file"])]
    pub description: Option<String>,
    #[arg(long, conflicts_with_all = ["clear_description", "description"])]
    pub description_file: Option<PathBuf>,
    #[arg(long)]
    pub clear_description: bool,
    #[arg(long, conflicts_with = "clear_deadline", help = DATE_HELP)]
    pub deadline: Option<String>,
    #[arg(long)]
    pub clear_deadline: bool,
}

#[derive(Debug, Args, Clone)]
pub struct ProjectArchiveArgs {
    pub project: String,
}

#[derive(Debug, Args, Clone)]
pub struct ProjectUnarchiveArgs {
    pub project: String,
}

#[derive(Debug, Args, Clone)]
pub struct UpcomingArgs {
    #[arg(long)]
    pub days: Option<i64>,
}

#[derive(Debug, Args, Clone, Default)]
pub struct ReviewArgs {
    #[arg(long = "next-action")]
    pub next_action: Vec<u64>,
    #[arg(long = "start")]
    pub start: Vec<u64>,
    #[arg(long = "waiting")]
    pub waiting: Vec<u64>,
    #[arg(long = "blocked")]
    pub blocked: Vec<u64>,
    #[arg(long = "complete")]
    pub complete: Vec<u64>,
    #[arg(long = "archive")]
    pub archive: Vec<u64>,
    #[arg(long = "defer", value_parser = parse_task_reschedule, help = REVIEW_DEFER_HELP)]
    pub defer: Vec<TaskReschedule>,
    #[arg(long = "plan", value_parser = parse_project_task_plan)]
    pub plan: Vec<ProjectTaskPlan>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TaskReschedule {
    pub id: u64,
    pub due_expression: String,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProjectTaskPlan {
    pub project_ref: String,
    pub title: String,
}

#[derive(Debug, Args, Clone)]
pub struct SearchArgs {
    pub query: String,
}

fn parse_task_reschedule(value: &str) -> Result<TaskReschedule, String> {
    let (id, due_expression) = value
        .split_once(':')
        .ok_or_else(|| format!("invalid defer instruction '{value}', expected ID:DATE"))?;

    let id = id
        .parse::<u64>()
        .map_err(|_| format!("invalid task id in defer instruction '{value}'"))?;

    Ok(TaskReschedule {
        id,
        due_expression: due_expression.trim().to_string(),
    })
}

fn parse_project_task_plan(value: &str) -> Result<ProjectTaskPlan, String> {
    let (project_ref, title) = value
        .split_once(':')
        .ok_or_else(|| format!("invalid plan instruction '{value}', expected PROJECT:TASK"))?;

    let project_ref = project_ref.trim();
    if project_ref.is_empty() {
        return Err(format!(
            "invalid project reference in plan instruction '{value}'"
        ));
    }

    let title = title.trim();
    if title.is_empty() {
        return Err(format!("invalid task title in plan instruction '{value}'"));
    }

    Ok(ProjectTaskPlan {
        project_ref: project_ref.to_string(),
        title: title.to_string(),
    })
}
