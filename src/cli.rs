use crate::domain::{Priority, RecurrenceRule, TaskStatus};
use chrono::NaiveDate;
use clap::{Args, Parser, Subcommand};

#[derive(Debug, Parser, Clone)]
#[command(
    name = "kelp",
    version,
    about = "A Rust-first CLI personal planner for tasks, projects, and reviews."
)]
pub struct Cli {
    #[command(subcommand)]
    pub command: Command,
}

#[derive(Debug, Subcommand, Clone)]
pub enum Command {
    Init,
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
pub enum TaskCommand {
    Add(TaskAddArgs),
    List(TaskListArgs),
    Show(TaskShowArgs),
    Edit(TaskEditArgs),
    Done(TaskDoneArgs),
    Reopen(TaskReopenArgs),
    Delete(TaskDeleteArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum ProjectCommand {
    Add(ProjectAddArgs),
    List(ProjectListArgs),
    Show(ProjectShowArgs),
    Archive(ProjectArchiveArgs),
}

#[derive(Debug, Subcommand, Clone)]
pub enum ReviewCommand {
    Daily(ListOutputArgs),
    Weekly(ListOutputArgs),
}

#[derive(Debug, Args, Clone, Default)]
pub struct ListOutputArgs {
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
    #[arg(long, value_parser = parse_date)]
    pub due: Option<NaiveDate>,
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
    #[arg(long, value_parser = parse_date, conflicts_with = "clear_due")]
    pub due: Option<NaiveDate>,
    #[arg(long)]
    pub clear_due: bool,
    #[arg(long, value_enum, conflicts_with = "clear_repeat")]
    pub repeat: Option<RecurrenceRule>,
    #[arg(long)]
    pub clear_repeat: bool,
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
pub struct UpcomingArgs {
    #[arg(long, default_value_t = 7)]
    pub days: i64,
    #[arg(long)]
    pub json: bool,
}

#[derive(Debug, Args, Clone)]
pub struct SearchArgs {
    pub query: String,
    #[arg(long)]
    pub json: bool,
}

fn parse_date(value: &str) -> Result<NaiveDate, String> {
    NaiveDate::parse_from_str(value, "%Y-%m-%d")
        .map_err(|_| format!("invalid date '{value}', expected YYYY-MM-DD"))
}
