use crate::error::ErrorReport;
use anyhow::Result;
use serde::Serialize;

pub const CURRENT_OUTPUT_SCHEMA_VERSION: u32 = 1;

#[derive(Debug, Clone, Serialize)]
pub struct JsonEnvelope<T> {
    pub schema_version: u32,
    pub command: String,
    pub data: T,
}

#[derive(Debug, Clone, Serialize)]
pub struct JsonErrorEnvelope {
    pub schema_version: u32,
    pub error: JsonErrorBody,
}

#[derive(Debug, Clone, Serialize)]
pub struct JsonErrorBody {
    pub code: String,
    pub message: String,
    pub details: Vec<String>,
}

#[derive(Debug, Clone)]
pub struct RunOutput {
    pub stdout: String,
    pub stderr: String,
    pub exit_code: i32,
}

impl RunOutput {
    pub fn success(stdout: String) -> Self {
        Self {
            stdout,
            stderr: String::new(),
            exit_code: 0,
        }
    }

    pub fn failure(stderr: String, exit_code: i32) -> Self {
        Self {
            stdout: String::new(),
            stderr,
            exit_code,
        }
    }
}

pub fn success_json<T: Serialize>(command: &str, data: T) -> Result<String> {
    serde_json::to_string_pretty(&JsonEnvelope {
        schema_version: CURRENT_OUTPUT_SCHEMA_VERSION,
        command: command.to_string(),
        data,
    })
    .map_err(Into::into)
}

pub fn error_json(report: &ErrorReport) -> String {
    serde_json::to_string_pretty(&JsonErrorEnvelope {
        schema_version: CURRENT_OUTPUT_SCHEMA_VERSION,
        error: JsonErrorBody {
            code: report.code.clone(),
            message: report.message.clone(),
            details: report.details.clone(),
        },
    })
    .expect("JSON error envelope serialization should succeed")
}

pub fn error_plain(report: &ErrorReport) -> String {
    if report.details.is_empty() {
        format!("error: {}", report.message)
    } else {
        format!(
            "error: {}\ncaused by:\n{}",
            report.message,
            report
                .details
                .iter()
                .map(|detail| format!("- {detail}"))
                .collect::<Vec<_>>()
                .join("\n")
        )
    }
}
