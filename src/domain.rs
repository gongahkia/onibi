use chrono::{Datelike, Duration, NaiveDate};
use clap::ValueEnum;
use serde::{Deserialize, Serialize};
use std::fmt;
use thiserror::Error;

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub struct TaskId(pub u64);

impl fmt::Display for TaskId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.0)
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub struct ProjectId(pub u64);

impl fmt::Display for ProjectId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.0)
    }
}

#[derive(
    Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize, ValueEnum, Default,
)]
#[serde(rename_all = "snake_case")]
pub enum Priority {
    Low,
    #[default]
    Medium,
    High,
}

impl Priority {
    pub fn rank(self) -> u8 {
        match self {
            Self::High => 3,
            Self::Medium => 2,
            Self::Low => 1,
        }
    }
}

impl fmt::Display for Priority {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let label = match self {
            Self::Low => "low",
            Self::Medium => "medium",
            Self::High => "high",
        };

        write!(f, "{label}")
    }
}

#[derive(
    Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize, ValueEnum, Default,
)]
#[serde(rename_all = "snake_case")]
pub enum TaskStatus {
    #[default]
    Todo,
    InProgress,
    Done,
    Archived,
}

impl TaskStatus {
    pub fn is_open(self) -> bool {
        matches!(self, Self::Todo | Self::InProgress)
    }
}

impl fmt::Display for TaskStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let label = match self {
            Self::Todo => "todo",
            Self::InProgress => "in_progress",
            Self::Done => "done",
            Self::Archived => "archived",
        };

        write!(f, "{label}")
    }
}

#[derive(
    Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize, ValueEnum,
)]
#[serde(rename_all = "snake_case")]
pub enum RecurrenceRule {
    Daily,
    Weekly,
    Monthly,
}

impl RecurrenceRule {
    pub fn next_due_date(self, current_due_date: NaiveDate) -> NaiveDate {
        match self {
            Self::Daily => current_due_date + Duration::days(1),
            Self::Weekly => current_due_date + Duration::weeks(1),
            Self::Monthly => add_one_month(current_due_date),
        }
    }
}

impl fmt::Display for RecurrenceRule {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let label = match self {
            Self::Daily => "daily",
            Self::Weekly => "weekly",
            Self::Monthly => "monthly",
        };

        write!(f, "{label}")
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(rename_all = "snake_case")]
pub enum ProjectStatus {
    #[default]
    Active,
    Archived,
}

impl fmt::Display for ProjectStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let label = match self {
            Self::Active => "active",
            Self::Archived => "archived",
        };

        write!(f, "{label}")
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Task {
    pub id: TaskId,
    pub title: String,
    pub notes: Option<String>,
    pub project_id: Option<ProjectId>,
    pub status: TaskStatus,
    pub priority: Priority,
    pub tags: Vec<String>,
    pub due_date: Option<NaiveDate>,
    pub recurrence: Option<RecurrenceRule>,
    pub created_on: NaiveDate,
    pub updated_on: NaiveDate,
    pub completed_on: Option<NaiveDate>,
}

impl Task {
    pub fn is_open(&self) -> bool {
        self.status.is_open()
    }

    pub fn matches_query(&self, query: &str) -> bool {
        let query = query.trim().to_lowercase();
        if query.is_empty() {
            return true;
        }

        let notes = self.notes.as_deref().unwrap_or_default().to_lowercase();
        let tags = self.tags.join(" ").to_lowercase();

        self.title.to_lowercase().contains(&query)
            || notes.contains(&query)
            || tags.contains(&query)
    }

    pub fn has_tag(&self, tag: &str) -> bool {
        let tag = tag.trim().to_lowercase();
        self.tags.iter().any(|existing| existing == &tag)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Project {
    pub id: ProjectId,
    pub name: String,
    pub description: Option<String>,
    pub status: ProjectStatus,
    pub created_on: NaiveDate,
    pub updated_on: NaiveDate,
}

impl Project {
    pub fn matches_query(&self, query: &str) -> bool {
        let query = query.trim().to_lowercase();
        if query.is_empty() {
            return true;
        }

        let description = self.description.as_deref().unwrap_or_default().to_lowercase();

        self.name.to_lowercase().contains(&query) || description.contains(&query)
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct AppState {
    pub schema_version: u32,
    pub next_task_id: u64,
    pub next_project_id: u64,
    pub tasks: Vec<Task>,
    pub projects: Vec<Project>,
}

impl Default for AppState {
    fn default() -> Self {
        Self {
            schema_version: 1,
            next_task_id: 1,
            next_project_id: 1,
            tasks: Vec::new(),
            projects: Vec::new(),
        }
    }
}

#[derive(Debug, Clone)]
pub struct NewTask {
    pub title: String,
    pub notes: Option<String>,
    pub project_id: Option<ProjectId>,
    pub priority: Priority,
    pub tags: Vec<String>,
    pub due_date: Option<NaiveDate>,
    pub recurrence: Option<RecurrenceRule>,
}

#[derive(Debug, Clone, Default)]
pub struct TaskPatch {
    pub title: Option<String>,
    pub notes: Option<Option<String>>,
    pub project_id: Option<Option<ProjectId>>,
    pub status: Option<TaskStatus>,
    pub priority: Option<Priority>,
    pub tags: Option<Vec<String>>,
    pub due_date: Option<Option<NaiveDate>>,
    pub recurrence: Option<Option<RecurrenceRule>>,
}

impl TaskPatch {
    pub fn is_empty(&self) -> bool {
        self.title.is_none()
            && self.notes.is_none()
            && self.project_id.is_none()
            && self.status.is_none()
            && self.priority.is_none()
            && self.tags.is_none()
            && self.due_date.is_none()
            && self.recurrence.is_none()
    }
}

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
pub struct ProjectSummary {
    pub total_tasks: usize,
    pub open_tasks: usize,
    pub completed_tasks: usize,
    pub overdue_tasks: usize,
    pub completion_percent: u8,
}

#[derive(Debug, Error)]
pub enum DomainError {
    #[error("task {0} does not exist")]
    TaskNotFound(TaskId),
    #[error("project reference '{0}' does not exist")]
    ProjectNotFound(String),
    #[error("project name '{0}' already exists")]
    DuplicateProject(String),
    #[error("{0} cannot be empty")]
    EmptyField(&'static str),
    #[error("recurring tasks require a due date")]
    RecurrenceRequiresDueDate,
    #[error("task {task_id} is already {status}")]
    TaskAlreadyClosed { task_id: TaskId, status: TaskStatus },
    #[error("project {project_id} is already archived")]
    ProjectAlreadyArchived { project_id: ProjectId },
    #[error("project {project_id} is already active")]
    ProjectAlreadyActive { project_id: ProjectId },
}

impl AppState {
    pub fn find_task(&self, task_id: TaskId) -> Option<&Task> {
        self.tasks.iter().find(|task| task.id == task_id)
    }

    pub fn find_task_mut(&mut self, task_id: TaskId) -> Option<&mut Task> {
        self.tasks.iter_mut().find(|task| task.id == task_id)
    }

    pub fn find_project(&self, project_id: ProjectId) -> Option<&Project> {
        self.projects.iter().find(|project| project.id == project_id)
    }

    pub fn find_project_mut(&mut self, project_id: ProjectId) -> Option<&mut Project> {
        self.projects.iter_mut().find(|project| project.id == project_id)
    }

    pub fn project_name(&self, project_id: ProjectId) -> Option<&str> {
        self.find_project(project_id).map(|project| project.name.as_str())
    }

    pub fn is_project_archived(&self, project_id: ProjectId) -> bool {
        self.find_project(project_id)
            .map(|project| matches!(project.status, ProjectStatus::Archived))
            .unwrap_or(false)
    }

    pub fn resolve_project_id(&self, reference: &str) -> Result<ProjectId, DomainError> {
        let reference = reference.trim();
        if reference.is_empty() {
            return Err(DomainError::EmptyField("project reference"));
        }

        if let Ok(raw_id) = reference.parse::<u64>() {
            let project_id = ProjectId(raw_id);
            if self.find_project(project_id).is_some() {
                return Ok(project_id);
            }
        }

        self.projects
            .iter()
            .find(|project| project.name.eq_ignore_ascii_case(reference))
            .map(|project| project.id)
            .ok_or_else(|| DomainError::ProjectNotFound(reference.to_string()))
    }

    pub fn create_project(
        &mut self,
        name: String,
        description: Option<String>,
        today: NaiveDate,
    ) -> Result<Project, DomainError> {
        let name = clean_required_text(name, "project name")?;
        if self
            .projects
            .iter()
            .any(|project| project.name.eq_ignore_ascii_case(&name))
        {
            return Err(DomainError::DuplicateProject(name));
        }

        let project = Project {
            id: ProjectId(self.next_project_id),
            name,
            description: clean_optional_text(description),
            status: ProjectStatus::Active,
            created_on: today,
            updated_on: today,
        };

        self.next_project_id += 1;
        self.projects.push(project.clone());

        Ok(project)
    }

    pub fn create_task(
        &mut self,
        input: NewTask,
        today: NaiveDate,
    ) -> Result<Task, DomainError> {
        let title = clean_required_text(input.title, "task title")?;
        let notes = clean_optional_text(input.notes);
        let tags = normalize_tags(input.tags);

        if input.recurrence.is_some() && input.due_date.is_none() {
            return Err(DomainError::RecurrenceRequiresDueDate);
        }

        if let Some(project_id) = input.project_id {
            if self.find_project(project_id).is_none() {
                return Err(DomainError::ProjectNotFound(project_id.to_string()));
            }
        }

        let task = Task {
            id: TaskId(self.next_task_id),
            title,
            notes,
            project_id: input.project_id,
            status: TaskStatus::Todo,
            priority: input.priority,
            tags,
            due_date: input.due_date,
            recurrence: input.recurrence,
            created_on: today,
            updated_on: today,
            completed_on: None,
        };

        self.next_task_id += 1;
        self.tasks.push(task.clone());

        Ok(task)
    }

    pub fn apply_task_patch(
        &mut self,
        task_id: TaskId,
        patch: TaskPatch,
        today: NaiveDate,
    ) -> Result<(), DomainError> {
        if let Some(Some(project_id)) = patch.project_id {
            if self.find_project(project_id).is_none() {
                return Err(DomainError::ProjectNotFound(project_id.to_string()));
            }
        }

        let task = self
            .find_task_mut(task_id)
            .ok_or(DomainError::TaskNotFound(task_id))?;

        if let Some(title) = patch.title {
            task.title = clean_required_text(title, "task title")?;
        }
        if let Some(notes) = patch.notes {
            task.notes = clean_optional_text(notes);
        }
        if let Some(project_id) = patch.project_id {
            task.project_id = project_id;
        }
        if let Some(priority) = patch.priority {
            task.priority = priority;
        }
        if let Some(tags) = patch.tags {
            task.tags = normalize_tags(tags);
        }
        if let Some(due_date) = patch.due_date {
            task.due_date = due_date;
        }
        if let Some(recurrence) = patch.recurrence {
            task.recurrence = recurrence;
        }

        if task.recurrence.is_some() && task.due_date.is_none() {
            return Err(DomainError::RecurrenceRequiresDueDate);
        }

        task.updated_on = today;

        Ok(())
    }

    pub fn set_task_status(
        &mut self,
        task_id: TaskId,
        status: TaskStatus,
        today: NaiveDate,
    ) -> Result<Option<TaskId>, DomainError> {
        match status {
            TaskStatus::Done => self.complete_task(task_id, today),
            TaskStatus::Todo | TaskStatus::InProgress => {
                let task = self
                    .find_task_mut(task_id)
                    .ok_or(DomainError::TaskNotFound(task_id))?;
                task.status = status;
                task.completed_on = None;
                task.updated_on = today;
                Ok(None)
            }
            TaskStatus::Archived => {
                let task = self
                    .find_task_mut(task_id)
                    .ok_or(DomainError::TaskNotFound(task_id))?;
                task.status = TaskStatus::Archived;
                task.updated_on = today;
                Ok(None)
            }
        }
    }

    pub fn delete_task(&mut self, task_id: TaskId) -> Result<Task, DomainError> {
        let index = self
            .tasks
            .iter()
            .position(|task| task.id == task_id)
            .ok_or(DomainError::TaskNotFound(task_id))?;

        Ok(self.tasks.remove(index))
    }

    pub fn archive_project(
        &mut self,
        project_id: ProjectId,
        today: NaiveDate,
    ) -> Result<(), DomainError> {
        let project = self
            .find_project_mut(project_id)
            .ok_or_else(|| DomainError::ProjectNotFound(project_id.to_string()))?;

        if matches!(project.status, ProjectStatus::Archived) {
            return Err(DomainError::ProjectAlreadyArchived { project_id });
        }

        project.status = ProjectStatus::Archived;
        project.updated_on = today;

        Ok(())
    }

    pub fn activate_project(
        &mut self,
        project_id: ProjectId,
        today: NaiveDate,
    ) -> Result<(), DomainError> {
        let project = self
            .find_project_mut(project_id)
            .ok_or_else(|| DomainError::ProjectNotFound(project_id.to_string()))?;

        if matches!(project.status, ProjectStatus::Active) {
            return Err(DomainError::ProjectAlreadyActive { project_id });
        }

        project.status = ProjectStatus::Active;
        project.updated_on = today;

        Ok(())
    }

    pub fn complete_task(
        &mut self,
        task_id: TaskId,
        today: NaiveDate,
    ) -> Result<Option<TaskId>, DomainError> {
        let index = self
            .tasks
            .iter()
            .position(|task| task.id == task_id)
            .ok_or(DomainError::TaskNotFound(task_id))?;

        let recurring_template = {
            let task = &mut self.tasks[index];
            if !task.status.is_open() {
                return Err(DomainError::TaskAlreadyClosed {
                    task_id,
                    status: task.status,
                });
            }

            task.status = TaskStatus::Done;
            task.completed_on = Some(today);
            task.updated_on = today;

            task.recurrence.map(|rule| {
                let due_date = task.due_date.ok_or(DomainError::RecurrenceRequiresDueDate)?;
                Ok((task.clone(), rule.next_due_date(due_date)))
            })
        }
        .transpose()?;

        if let Some((template, next_due_date)) = recurring_template {
            let next_task = Task {
                id: TaskId(self.next_task_id),
                title: template.title,
                notes: template.notes,
                project_id: template.project_id,
                status: TaskStatus::Todo,
                priority: template.priority,
                tags: template.tags,
                due_date: Some(next_due_date),
                recurrence: template.recurrence,
                created_on: today,
                updated_on: today,
                completed_on: None,
            };

            let next_task_id = next_task.id;
            self.next_task_id += 1;
            self.tasks.push(next_task);
            return Ok(Some(next_task_id));
        }

        Ok(None)
    }

    pub fn project_tasks(&self, project_id: ProjectId) -> Vec<&Task> {
        self.tasks
            .iter()
            .filter(|task| task.project_id == Some(project_id))
            .collect()
    }

    pub fn project_summary(
        &self,
        project_id: ProjectId,
        today: NaiveDate,
    ) -> Result<ProjectSummary, DomainError> {
        if self.find_project(project_id).is_none() {
            return Err(DomainError::ProjectNotFound(project_id.to_string()));
        }

        let tasks: Vec<&Task> = self
            .tasks
            .iter()
            .filter(|task| {
                task.project_id == Some(project_id) && !matches!(task.status, TaskStatus::Archived)
            })
            .collect();

        let total_tasks = tasks.len();
        let completed_tasks = tasks
            .iter()
            .filter(|task| matches!(task.status, TaskStatus::Done))
            .count();
        let open_tasks = tasks.iter().filter(|task| task.is_open()).count();
        let overdue_tasks = tasks
            .iter()
            .filter(|task| {
                task.is_open()
                    && task
                        .due_date
                        .map(|due_date| due_date < today)
                        .unwrap_or(false)
            })
            .count();
        let completion_percent = if total_tasks == 0 {
            0
        } else {
            ((completed_tasks * 100) / total_tasks) as u8
        };

        Ok(ProjectSummary {
            total_tasks,
            open_tasks,
            completed_tasks,
            overdue_tasks,
            completion_percent,
        })
    }
}

pub fn normalize_tags(tags: Vec<String>) -> Vec<String> {
    let mut normalized = Vec::new();

    for tag in tags {
        let cleaned = tag.trim().to_lowercase();
        if cleaned.is_empty() || normalized.iter().any(|existing| existing == &cleaned) {
            continue;
        }

        normalized.push(cleaned);
    }

    normalized
}

fn clean_required_text(value: String, field_name: &'static str) -> Result<String, DomainError> {
    let cleaned = value.trim().to_string();
    if cleaned.is_empty() {
        return Err(DomainError::EmptyField(field_name));
    }

    Ok(cleaned)
}

fn clean_optional_text(value: Option<String>) -> Option<String> {
    value.and_then(|text| {
        let cleaned = text.trim().to_string();
        if cleaned.is_empty() {
            None
        } else {
            Some(cleaned)
        }
    })
}

fn add_one_month(date: NaiveDate) -> NaiveDate {
    let (year, month) = if date.month() == 12 {
        (date.year() + 1, 1)
    } else {
        (date.year(), date.month() + 1)
    };

    let day = date.day().min(days_in_month(year, month));
    NaiveDate::from_ymd_opt(year, month, day).expect("month arithmetic should produce a valid date")
}

fn days_in_month(year: i32, month: u32) -> u32 {
    let first_of_next_month = if month == 12 {
        NaiveDate::from_ymd_opt(year + 1, 1, 1)
    } else {
        NaiveDate::from_ymd_opt(year, month + 1, 1)
    }
    .expect("month boundaries should be valid");

    (first_of_next_month - Duration::days(1)).day()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn date(value: &str) -> NaiveDate {
        NaiveDate::parse_from_str(value, "%Y-%m-%d").expect("date fixture should be valid")
    }

    #[test]
    fn monthly_recurrence_clamps_to_month_end() {
        assert_eq!(
            RecurrenceRule::Monthly.next_due_date(date("2026-01-31")),
            date("2026-02-28")
        );
    }

    #[test]
    fn completing_recurring_task_spawns_the_next_instance() {
        let mut state = AppState::default();
        let created_on = date("2026-03-14");
        let task = state
            .create_task(
                NewTask {
                    title: "Write status report".to_string(),
                    notes: Some("Ship the weekly update".to_string()),
                    project_id: None,
                    priority: Priority::High,
                    tags: vec!["ops".to_string()],
                    due_date: Some(created_on),
                    recurrence: Some(RecurrenceRule::Weekly),
                },
                created_on,
            )
            .expect("task creation should succeed");

        let spawned = state
            .complete_task(task.id, created_on)
            .expect("completion should succeed");

        assert_eq!(spawned, Some(TaskId(2)));
        assert_eq!(state.tasks.len(), 2);
        assert_eq!(state.tasks[0].status, TaskStatus::Done);
        assert_eq!(state.tasks[1].due_date, Some(date("2026-03-21")));
        assert_eq!(state.tasks[1].status, TaskStatus::Todo);
    }

    #[test]
    fn project_summary_counts_open_done_and_overdue_tasks() {
        let mut state = AppState::default();
        let today = date("2026-03-14");
        let project = state
            .create_project("Launch".to_string(), None, today)
            .expect("project creation should succeed");

        let overdue_task = state
            .create_task(
                NewTask {
                    title: "Patch release notes".to_string(),
                    notes: None,
                    project_id: Some(project.id),
                    priority: Priority::Medium,
                    tags: vec!["release".to_string()],
                    due_date: Some(date("2026-03-10")),
                    recurrence: None,
                },
                today,
            )
            .expect("task creation should succeed");

        let completed_task = state
            .create_task(
                NewTask {
                    title: "Publish changelog".to_string(),
                    notes: None,
                    project_id: Some(project.id),
                    priority: Priority::Low,
                    tags: vec!["release".to_string()],
                    due_date: Some(today),
                    recurrence: None,
                },
                today,
            )
            .expect("task creation should succeed");

        state
            .complete_task(completed_task.id, today)
            .expect("completion should succeed");

        let summary = state
            .project_summary(project.id, today)
            .expect("summary should succeed");

        assert_eq!(summary.total_tasks, 2);
        assert_eq!(summary.completed_tasks, 1);
        assert_eq!(summary.open_tasks, 1);
        assert_eq!(summary.overdue_tasks, 1);
        assert_eq!(summary.completion_percent, 50);
        assert_eq!(
            state
                .find_task(overdue_task.id)
                .expect("task should still exist")
                .status,
            TaskStatus::Todo
        );
    }
}
