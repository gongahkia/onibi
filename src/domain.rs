use chrono::{Datelike, Duration, NaiveDate};
use clap::ValueEnum;
use serde::{Deserialize, Serialize};
use std::collections::HashSet;
use std::fmt;
use thiserror::Error;

pub const CURRENT_APP_SCHEMA_VERSION: u32 = 5;

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
    NextAction,
    InProgress,
    Waiting,
    Blocked,
    Done,
    Archived,
}

impl TaskStatus {
    pub fn is_open(self) -> bool {
        matches!(
            self,
            Self::Todo | Self::NextAction | Self::InProgress | Self::Waiting | Self::Blocked
        )
    }

    pub fn is_next_action(self) -> bool {
        matches!(self, Self::NextAction | Self::InProgress)
    }
}

impl fmt::Display for TaskStatus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let label = match self {
            Self::Todo => "todo",
            Self::NextAction => "next_action",
            Self::InProgress => "in_progress",
            Self::Waiting => "waiting",
            Self::Blocked => "blocked",
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
    #[serde(default)]
    pub archived_on: Option<NaiveDate>,
    #[serde(default)]
    pub waiting_until: Option<NaiveDate>,
    #[serde(default)]
    pub blocked_reason: Option<String>,
    #[serde(default)]
    pub depends_on: Vec<TaskId>,
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
        let blocked_reason = self
            .blocked_reason
            .as_deref()
            .unwrap_or_default()
            .to_lowercase();
        let tags = self.tags.join(" ").to_lowercase();

        self.title.to_lowercase().contains(&query)
            || notes.contains(&query)
            || blocked_reason.contains(&query)
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
    #[serde(default)]
    pub archived_on: Option<NaiveDate>,
    #[serde(default)]
    pub deadline: Option<NaiveDate>,
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
    #[serde(default = "current_app_schema_version")]
    pub schema_version: u32,
    pub next_task_id: u64,
    pub next_project_id: u64,
    pub tasks: Vec<Task>,
    pub projects: Vec<Project>,
}

impl Default for AppState {
    fn default() -> Self {
        Self {
            schema_version: CURRENT_APP_SCHEMA_VERSION,
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
    pub waiting_until: Option<NaiveDate>,
    pub blocked_reason: Option<String>,
    pub depends_on: Vec<TaskId>,
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
    pub waiting_until: Option<Option<NaiveDate>>,
    pub blocked_reason: Option<Option<String>>,
    pub depends_on: Option<Vec<TaskId>>,
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
            && self.waiting_until.is_none()
            && self.blocked_reason.is_none()
            && self.depends_on.is_none()
    }
}

#[derive(Debug, Clone, Default)]
pub struct ProjectPatch {
    pub description: Option<Option<String>>,
    pub deadline: Option<Option<NaiveDate>>,
}

impl ProjectPatch {
    pub fn is_empty(&self) -> bool {
        self.description.is_none() && self.deadline.is_none()
    }
}

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
pub struct ProjectSummary {
    pub total_tasks: usize,
    pub open_tasks: usize,
    pub completed_tasks: usize,
    pub overdue_tasks: usize,
    pub next_action_tasks: usize,
    pub waiting_tasks: usize,
    pub blocked_tasks: usize,
    pub dependency_blocked_tasks: usize,
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
    #[error("task dependency {0} does not exist")]
    InvalidTaskDependency(TaskId),
    #[error("task {task_id} cannot depend on {dependency_id} because it creates a cycle")]
    TaskDependencyCycle {
        task_id: TaskId,
        dependency_id: TaskId,
    },
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
        deadline: Option<NaiveDate>,
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
            archived_on: None,
            deadline,
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
        let depends_on = self.validate_task_dependencies(None, &input.depends_on)?;

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
            archived_on: None,
            waiting_until: input.waiting_until,
            blocked_reason: clean_optional_text(input.blocked_reason),
            depends_on,
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
        let normalized_dependencies = patch
            .depends_on
            .as_ref()
            .map(|dependencies| self.validate_task_dependencies(Some(task_id), dependencies))
            .transpose()?;

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
        if let Some(waiting_until) = patch.waiting_until {
            task.waiting_until = waiting_until;
        }
        if let Some(blocked_reason) = patch.blocked_reason {
            task.blocked_reason = clean_optional_text(blocked_reason);
        }
        if let Some(depends_on) = normalized_dependencies {
            task.depends_on = depends_on;
        }

        if task.recurrence.is_some() && task.due_date.is_none() {
            return Err(DomainError::RecurrenceRequiresDueDate);
        }

        task.updated_on = today;
        let canonical_project_id = matches!(task.status, TaskStatus::NextAction).then_some(task.project_id);

        let canonical_project_id = canonical_project_id.flatten();
        if let Some(project_id) = canonical_project_id {
            self.demote_other_next_actions_in_project(project_id, task_id, today);
        }

        Ok(())
    }

    pub fn apply_project_patch(
        &mut self,
        project_id: ProjectId,
        patch: ProjectPatch,
        today: NaiveDate,
    ) -> Result<(), DomainError> {
        let project = self
            .find_project_mut(project_id)
            .ok_or_else(|| DomainError::ProjectNotFound(project_id.to_string()))?;

        if let Some(description) = patch.description {
            project.description = clean_optional_text(description);
        }
        if let Some(deadline) = patch.deadline {
            project.deadline = deadline;
        }
        project.updated_on = today;

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
            TaskStatus::NextAction => {
                let project_id = {
                    let task = self
                        .find_task_mut(task_id)
                        .ok_or(DomainError::TaskNotFound(task_id))?;
                    task.status = TaskStatus::NextAction;
                    task.completed_on = None;
                    task.archived_on = None;
                    task.waiting_until = None;
                    task.blocked_reason = None;
                    task.updated_on = today;
                    task.project_id
                };
                if let Some(project_id) = project_id {
                    self.demote_other_next_actions_in_project(project_id, task_id, today);
                }
                Ok(None)
            }
            TaskStatus::Todo | TaskStatus::InProgress | TaskStatus::Waiting | TaskStatus::Blocked => {
                let task = self
                    .find_task_mut(task_id)
                    .ok_or(DomainError::TaskNotFound(task_id))?;
                task.status = status;
                task.completed_on = None;
                task.archived_on = None;
                if !matches!(status, TaskStatus::Waiting) {
                    task.waiting_until = None;
                }
                if !matches!(status, TaskStatus::Blocked) {
                    task.blocked_reason = None;
                }
                task.updated_on = today;
                Ok(None)
            }
            TaskStatus::Archived => {
                let task = self
                    .find_task_mut(task_id)
                    .ok_or(DomainError::TaskNotFound(task_id))?;
                task.status = TaskStatus::Archived;
                task.archived_on = Some(today);
                task.waiting_until = None;
                task.blocked_reason = None;
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

        let removed = self.tasks.remove(index);
        for task in &mut self.tasks {
            task.depends_on.retain(|dependency| *dependency != task_id);
        }

        Ok(removed)
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
        project.archived_on = Some(today);
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
        project.archived_on = None;
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
            task.waiting_until = None;
            task.blocked_reason = None;
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
                archived_on: None,
                waiting_until: None,
                blocked_reason: None,
                depends_on: Vec::new(),
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

    pub fn unresolved_task_dependencies(&self, task: &Task) -> Vec<TaskId> {
        task.depends_on
            .iter()
            .copied()
            .filter(|dependency| {
                self.find_task(*dependency)
                    .map(|task| task.status.is_open())
                    .unwrap_or(true)
            })
            .collect()
    }

    pub fn has_unresolved_dependencies(&self, task: &Task) -> bool {
        !self.unresolved_task_dependencies(task).is_empty()
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
        let next_action_tasks = tasks
            .iter()
            .filter(|task| task.status.is_next_action())
            .count();
        let waiting_tasks = tasks
            .iter()
            .filter(|task| matches!(task.status, TaskStatus::Waiting))
            .count();
        let blocked_tasks = tasks
            .iter()
            .filter(|task| matches!(task.status, TaskStatus::Blocked))
            .count();
        let dependency_blocked_tasks = tasks
            .iter()
            .filter(|task| task.is_open() && self.has_unresolved_dependencies(task))
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
            next_action_tasks,
            waiting_tasks,
            blocked_tasks,
            dependency_blocked_tasks,
            completion_percent,
        })
    }

    fn validate_task_dependencies(
        &self,
        task_id: Option<TaskId>,
        dependencies: &[TaskId],
    ) -> Result<Vec<TaskId>, DomainError> {
        let normalized = normalize_task_dependencies(dependencies.to_vec());

        for dependency in &normalized {
            if self.find_task(*dependency).is_none() {
                return Err(DomainError::InvalidTaskDependency(*dependency));
            }
        }

        if let Some(task_id) = task_id {
            for dependency in &normalized {
                if *dependency == task_id
                    || self.task_reaches_target(*dependency, task_id, task_id, &normalized)
                {
                    return Err(DomainError::TaskDependencyCycle {
                        task_id,
                        dependency_id: *dependency,
                    });
                }
            }
        }

        Ok(normalized)
    }

    fn demote_other_next_actions_in_project(
        &mut self,
        project_id: ProjectId,
        keep_task_id: TaskId,
        today: NaiveDate,
    ) {
        for task in &mut self.tasks {
            if task.id != keep_task_id
                && task.project_id == Some(project_id)
                && matches!(task.status, TaskStatus::NextAction)
            {
                task.status = TaskStatus::Todo;
                task.updated_on = today;
            }
        }
    }

    fn task_reaches_target(
        &self,
        start: TaskId,
        target: TaskId,
        patched_task_id: TaskId,
        patched_dependencies: &[TaskId],
    ) -> bool {
        let mut visited = HashSet::new();
        self.task_reaches_target_inner(
            start,
            target,
            patched_task_id,
            patched_dependencies,
            &mut visited,
        )
    }

    fn task_reaches_target_inner(
        &self,
        current: TaskId,
        target: TaskId,
        patched_task_id: TaskId,
        patched_dependencies: &[TaskId],
        visited: &mut HashSet<TaskId>,
    ) -> bool {
        if !visited.insert(current) {
            return false;
        }

        let dependencies = if current == patched_task_id {
            patched_dependencies.to_vec()
        } else if let Some(task) = self.find_task(current) {
            task.depends_on.clone()
        } else {
            Vec::new()
        };

        for dependency in dependencies {
            if dependency == target
                || self.task_reaches_target_inner(
                    dependency,
                    target,
                    patched_task_id,
                    patched_dependencies,
                    visited,
                )
            {
                return true;
            }
        }

        false
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

fn normalize_task_dependencies(dependencies: Vec<TaskId>) -> Vec<TaskId> {
    let mut normalized = Vec::new();

    for dependency in dependencies {
        if normalized.iter().any(|existing| existing == &dependency) {
            continue;
        }

        normalized.push(dependency);
    }

    normalized.sort();
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

fn current_app_schema_version() -> u32 {
    CURRENT_APP_SCHEMA_VERSION
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
                    waiting_until: None,
                    blocked_reason: None,
                    depends_on: Vec::new(),
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
            .create_project("Launch".to_string(), None, Some(date("2026-03-28")), today)
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
                    waiting_until: None,
                    blocked_reason: None,
                    depends_on: Vec::new(),
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
                    waiting_until: None,
                    blocked_reason: None,
                    depends_on: Vec::new(),
                },
                today,
            )
            .expect("task creation should succeed");

        let waiting_task = state
            .create_task(
                NewTask {
                    title: "Wait for legal sign-off".to_string(),
                    notes: None,
                    project_id: Some(project.id),
                    priority: Priority::Low,
                    tags: vec!["legal".to_string()],
                    due_date: Some(date("2026-03-15")),
                    recurrence: None,
                    waiting_until: None,
                    blocked_reason: None,
                    depends_on: Vec::new(),
                },
                today,
            )
            .expect("task creation should succeed");

        state
            .complete_task(completed_task.id, today)
            .expect("completion should succeed");
        state
            .set_task_status(overdue_task.id, TaskStatus::NextAction, today)
            .expect("status update should succeed");
        state
            .set_task_status(waiting_task.id, TaskStatus::Waiting, today)
            .expect("status update should succeed");

        let summary = state
            .project_summary(project.id, today)
            .expect("summary should succeed");

        assert_eq!(summary.total_tasks, 3);
        assert_eq!(summary.completed_tasks, 1);
        assert_eq!(summary.open_tasks, 2);
        assert_eq!(summary.overdue_tasks, 1);
        assert_eq!(summary.next_action_tasks, 1);
        assert_eq!(summary.waiting_tasks, 1);
        assert_eq!(summary.blocked_tasks, 0);
        assert_eq!(summary.dependency_blocked_tasks, 0);
        assert_eq!(summary.completion_percent, 33);
        assert_eq!(
            state
                .find_task(overdue_task.id)
                .expect("task should still exist")
                .status,
            TaskStatus::NextAction
        );
        assert_eq!(
            state
                .find_project(project.id)
                .expect("project should exist")
                .deadline,
            Some(date("2026-03-28"))
        );
    }

    #[test]
    fn setting_task_status_clears_waiting_and_blocked_metadata_when_work_resumes() {
        let mut state = AppState::default();
        let today = date("2026-03-14");
        let task = state
            .create_task(
                NewTask {
                    title: "Unstick deployment".to_string(),
                    notes: None,
                    project_id: None,
                    priority: Priority::High,
                    tags: vec!["ops".to_string()],
                    due_date: Some(today),
                    recurrence: None,
                    waiting_until: Some(date("2026-03-16")),
                    blocked_reason: Some("Waiting for vendor response".to_string()),
                    depends_on: Vec::new(),
                },
                today,
            )
            .expect("task creation should succeed");

        state
            .set_task_status(task.id, TaskStatus::NextAction, today)
            .expect("status update should succeed");

        let task = state.find_task(task.id).expect("task should still exist");
        assert_eq!(task.waiting_until, None);
        assert_eq!(task.blocked_reason, None);
    }

    #[test]
    fn tasks_can_depend_on_other_tasks_without_creating_cycles() {
        let mut state = AppState::default();
        let today = date("2026-03-14");
        let first = state
            .create_task(
                NewTask {
                    title: "Prepare brief".to_string(),
                    notes: None,
                    project_id: None,
                    priority: Priority::Medium,
                    tags: vec![],
                    due_date: Some(today),
                    recurrence: None,
                    waiting_until: None,
                    blocked_reason: None,
                    depends_on: Vec::new(),
                },
                today,
            )
            .expect("first task should be created");
        let second = state
            .create_task(
                NewTask {
                    title: "Send brief".to_string(),
                    notes: None,
                    project_id: None,
                    priority: Priority::Medium,
                    tags: vec![],
                    due_date: Some(today),
                    recurrence: None,
                    waiting_until: None,
                    blocked_reason: None,
                    depends_on: vec![first.id],
                },
                today,
            )
            .expect("dependent task should be created");

        assert_eq!(state.unresolved_task_dependencies(&state.tasks[1]), vec![first.id]);
        state
            .complete_task(first.id, today)
            .expect("completion should succeed");
        assert!(state.unresolved_task_dependencies(&state.tasks[1]).is_empty());

        let error = state
            .apply_task_patch(
                first.id,
                TaskPatch {
                    depends_on: Some(vec![second.id]),
                    ..TaskPatch::default()
                },
                today,
            )
            .expect_err("cycle should be rejected");

        assert!(matches!(error, DomainError::TaskDependencyCycle { .. }));
    }

    #[test]
    fn marking_a_project_task_as_next_action_demotes_the_previous_one() {
        let mut state = AppState::default();
        let today = date("2026-03-14");
        let project = state
            .create_project(
                NewProject {
                    name: "Launch".to_string(),
                    description: None,
                    deadline: None,
                },
                today,
            )
            .expect("project creation should succeed");
        let first = state
            .create_task(
                NewTask {
                    title: "Draft copy".to_string(),
                    notes: None,
                    project_id: Some(project.id),
                    priority: Priority::Medium,
                    tags: vec![],
                    due_date: None,
                    recurrence: None,
                    waiting_until: None,
                    blocked_reason: None,
                    depends_on: Vec::new(),
                },
                today,
            )
            .expect("first task should be created");
        let second = state
            .create_task(
                NewTask {
                    title: "Publish copy".to_string(),
                    notes: None,
                    project_id: Some(project.id),
                    priority: Priority::Medium,
                    tags: vec![],
                    due_date: None,
                    recurrence: None,
                    waiting_until: None,
                    blocked_reason: None,
                    depends_on: Vec::new(),
                },
                today,
            )
            .expect("second task should be created");

        state
            .set_task_status(first.id, TaskStatus::NextAction, today)
            .expect("first next action should succeed");
        state
            .set_task_status(second.id, TaskStatus::NextAction, today)
            .expect("second next action should succeed");

        assert_eq!(
            state.find_task(first.id).expect("first task should exist").status,
            TaskStatus::Todo
        );
        assert_eq!(
            state.find_task(second.id).expect("second task should exist").status,
            TaskStatus::NextAction
        );
    }
}
