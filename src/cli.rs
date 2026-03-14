use crate::config::TaskSortKey;
use crate::domain::{Priority, RecurrenceRule, TaskStatus};
use clap::{Args, Parser, Subcommand};
use std::path::PathBuf;

#[derive(Debug, Parser, Clone)]
#[command(
    name = "kelp",
    version,
    about = "A Rust-first CLI personal planner for tasks, projects, reviews, and data workflows."
)]
pub struct Cli {
    #[command(subcommand)]
    pub command: Command,
}

#[derive(Debug, Subcommand, Clone)]
pub enum Command {
    Init,
    Config {
        #[command(subcommand)]
        command: ConfigCommand,
    },
    Import {
        #[command(subcommand)]
        command: ImportCommand,
    },
    Storage {
        #[command(subcommand)]
        command: StorageCommand,
    },
    Task {
        #[command(subcommand)]
        command: TaskCommand,
    },
    Project {
        #[command(subcommand)]
        command: ProjectCommand,
    },
    Today(ListOutputArgs),
    Upcoming(UpcomingArgs),
    Review {
        #[command(subcommand)]
        command: ReviewCommand,
    },
    Search(SearchArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum ConfigCommand {
    Show(ConfigShowArgs),
    Set(ConfigSetArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum ImportCommand {
    Legacy(LegacyImportArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum StorageCommand {
    Path(StoragePathArgs),
    Export(StorageExportArgs),
    Backup(StorageBackupArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum TaskCommand {
    Add(TaskAddArgs),
    List(TaskListArgs),
    Show(TaskShowArgs),
    Edit(TaskEditArgs),
    BulkEdit(TaskBulkEditArgs),
    Start(TaskStartArgs),
    Done(TaskDoneArgs),
    Reopen(TaskReopenArgs),
    Defer(TaskDeferArgs),
    Archive(TaskArchiveArgs),
    Unarchive(TaskUnarchiveArgs),
    Delete(TaskDeleteArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum ProjectCommand {
    Add(ProjectAddArgs),
    List(ProjectListArgs),
    Show(ProjectShowArgs),
    Archive(ProjectArchiveArgs),
    Unarchive(ProjectUnarchiveArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum ReviewCommand {
    Daily(ReviewArgs),
    Weekly(ReviewArgs),
}

#[derive(Debug, Args, Clone, Default)]
pub struct ListOutputArgs {
    #[arg(long)]
    pub json: bool,
}

#[derive(Debug, Args, Clone)]
pub struct ConfigShowArgs {
    #[arg(long)]
    pub json: bool,
}

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
pub struct LegacyImportArgs {
    #[arg(long, default_value = ".")]
    pub source: PathBuf,
    #[arg(long)]
    pub json: bool,
}

#[derive(Debug, Args, Clone)]
pub struct StoragePathArgs {
    #[arg(long)]
    pub json: bool,
}

#[derive(Debug, Args, Clone)]
pub struct StorageExportArgs {
    #[arg(long)]
    pub output: PathBuf,
    #[arg(long)]
    pub json: bool,
}

#[derive(Debug, Args, Clone)]
pub struct StorageBackupArgs {
    #[arg(long)]
    pub json: bool,
}

#[derive(Debug, Args, Clone)]
pub struct TaskAddArgs {
    #[arg(long)]
    pub title: String,
    #[arg(long)]
    pub notes: Option<String>,
    #[arg(long)]
    pub project: Option<String>,
    #[arg(long, value_enum, default_value_t = Priority::Medium)]
    pub priority: Priority,
    #[arg(long = "tag")]
    pub tags: Vec<String>,
    #[arg(long)]
    pub due: Option<String>,
    #[arg(long, value_enum)]
    pub repeat: Option<RecurrenceRule>,
}

#[derive(Debug, Args, Clone)]
pub struct TaskListArgs {
    #[arg(long)]
    pub project: Option<String>,
    #[arg(long, value_enum)]
    pub status: Option<TaskStatus>,
    #[arg(long, value_enum)]
    pub priority: Option<Priority>,
    #[arg(long)]
    pub tag: Option<String>,
    #[arg(long)]
    pub due_today: bool,
    #[arg(long)]
    pub overdue: bool,
    #[arg(long)]
    pub all: bool,
    #[arg(long, value_enum)]
    pub sort: Option<TaskSortKey>,
    #[arg(long)]
    pub json: bool,
}

#[derive(Debug, Args, Clone)]
pub struct TaskShowArgs {
    pub id: u64,
    #[arg(long)]
    pub json: bool,
}

#[derive(Debug, Args, Clone)]
pub struct TaskEditArgs {
    pub id: u64,
    #[arg(long)]
    pub title: Option<String>,
    #[arg(long, conflicts_with = "clear_notes")]
    pub notes: Option<String>,
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
    #[arg(long, conflicts_with = "clear_due")]
    pub due: Option<String>,
    #[arg(long)]
    pub clear_due: bool,
    #[arg(long, value_enum, conflicts_with = "clear_repeat")]
    pub repeat: Option<RecurrenceRule>,
    #[arg(long)]
    pub clear_repeat: bool,
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
    #[arg(long, conflicts_with = "clear_due")]
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
    #[arg(long, conflicts_with = "days")]
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
    #[arg(long)]
    pub description: Option<String>,
}

#[derive(Debug, Args, Clone)]
pub struct ProjectListArgs {
    #[arg(long)]
    pub archived: bool,
    #[arg(long)]
    pub json: bool,
}

#[derive(Debug, Args, Clone)]
pub struct ProjectShowArgs {
    pub project: String,
    #[arg(long)]
    pub json: bool,
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
    #[arg(long)]
    pub json: bool,
}

#[derive(Debug, Args, Clone)]
pub struct ReviewArgs {
    #[arg(long)]
    pub json: bool,
    #[arg(long = "start")]
    pub start: Vec<u64>,
    #[arg(long = "complete")]
    pub complete: Vec<u64>,
    #[arg(long = "archive")]
    pub archive: Vec<u64>,
    #[arg(long = "defer", value_parser = parse_task_reschedule)]
    pub defer: Vec<TaskReschedule>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TaskReschedule {
    pub id: u64,
    pub due_expression: String,
}

#[derive(Debug, Args, Clone)]
pub struct SearchArgs {
    pub query: String,
    #[arg(long)]
    pub json: bool,
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
