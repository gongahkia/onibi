use crate::domain::DomainError;
use serde::Serialize;
use thiserror::Error;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "snake_case")]
pub enum ErrorCategory {
    Usage,
    NotFound,
    Conflict,
    Storage,
}

impl ErrorCategory {
    pub fn exit_code(self) -> i32 {
        match self {
            Self::Usage => 2,
            Self::NotFound => 3,
            Self::Conflict => 4,
            Self::Storage => 5,
        }
    }
}

#[derive(Debug, Error)]
#[error("{message}")]
pub struct KelpCliError {
    pub category: ErrorCategory,
    pub code: &'static str,
    pub message: String,
}

impl KelpCliError {
    pub fn new(category: ErrorCategory, code: &'static str, message: impl Into<String>) -> Self {
        Self {
            category,
            code,
            message: message.into(),
        }
    }
}

#[derive(Debug, Clone, Serialize)]
pub struct ErrorReport {
    pub exit_code: i32,
    pub code: String,
    pub message: String,
    pub details: Vec<String>,
}

pub fn usage_error(code: &'static str, message: impl Into<String>) -> anyhow::Error {
    KelpCliError::new(ErrorCategory::Usage, code, message).into()
}

pub fn not_found_error(code: &'static str, message: impl Into<String>) -> anyhow::Error {
    KelpCliError::new(ErrorCategory::NotFound, code, message).into()
}

pub fn conflict_error(code: &'static str, message: impl Into<String>) -> anyhow::Error {
    KelpCliError::new(ErrorCategory::Conflict, code, message).into()
}

pub fn classify_error(error: &anyhow::Error) -> ErrorReport {
    if let Some(custom) = find_in_chain::<KelpCliError>(error) {
        return report_from_category(custom.category, custom.code, error);
    }

    if let Some(domain) = find_in_chain::<DomainError>(error) {
        let (category, code) = match domain {
            DomainError::TaskNotFound(_) => (ErrorCategory::NotFound, "task_not_found"),
            DomainError::ProjectNotFound(_) => (ErrorCategory::NotFound, "project_not_found"),
            DomainError::InvalidTaskDependency(_) => {
                (ErrorCategory::NotFound, "task_dependency_not_found")
            }
            DomainError::DuplicateProject(_) => (ErrorCategory::Conflict, "duplicate_project"),
            DomainError::EmptyField(_) => (ErrorCategory::Usage, "empty_field"),
            DomainError::RecurrenceRequiresDueDate => {
                (ErrorCategory::Usage, "recurrence_requires_due_date")
            }
            DomainError::TaskAlreadyClosed { .. } => {
                (ErrorCategory::Conflict, "task_already_closed")
            }
            DomainError::ProjectAlreadyArchived { .. } => {
                (ErrorCategory::Conflict, "project_already_archived")
            }
            DomainError::ProjectAlreadyActive { .. } => {
                (ErrorCategory::Conflict, "project_already_active")
            }
            DomainError::TaskDependencyCycle { .. } => {
                (ErrorCategory::Conflict, "task_dependency_cycle")
            }
        };
        return report_from_category(category, code, error);
    }

    if find_in_chain::<clap::Error>(error).is_some() {
        return report_from_category(ErrorCategory::Usage, "usage_error", error);
    }

    if find_in_chain::<std::io::Error>(error).is_some()
        || find_in_chain::<serde_json::Error>(error).is_some()
    {
        return report_from_category(ErrorCategory::Storage, "storage_error", error);
    }

    report_from_category(ErrorCategory::Storage, "storage_error", error)
}

fn report_from_category(category: ErrorCategory, code: &str, error: &anyhow::Error) -> ErrorReport {
    let details = error
        .chain()
        .skip(1)
        .map(|cause| cause.to_string())
        .filter(|detail| !detail.trim().is_empty())
        .collect::<Vec<_>>();

    ErrorReport {
        exit_code: category.exit_code(),
        code: code.to_string(),
        message: error.to_string(),
        details,
    }
}

fn find_in_chain<T>(error: &anyhow::Error) -> Option<&T>
where
    T: std::error::Error + 'static,
{
    error.chain().find_map(|cause| cause.downcast_ref::<T>())
}
