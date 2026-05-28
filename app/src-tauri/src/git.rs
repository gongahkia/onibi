use serde::Serialize;
use std::{
    path::{Component, Path, PathBuf},
    process::{Command, Output},
};

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GitStatus {
    is_repo: bool,
    repo_root: Option<PathBuf>,
    branch: Option<String>,
    upstream: Option<String>,
    ahead: u32,
    behind: u32,
    entries: Vec<GitStatusEntry>,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GitStatusEntry {
    path: String,
    original_path: Option<String>,
    full_path: PathBuf,
    index_status: Option<String>,
    worktree_status: Option<String>,
}

fn command_text(args: &[&str]) -> String {
    format!("git {}", args.join(" "))
}

fn git_output(root: &Path, args: &[&str]) -> Result<Output, String> {
    Command::new("git")
        .arg("-C")
        .arg(root)
        .args(args)
        .output()
        .map_err(|err| format!("failed to run {}: {err}", command_text(args)))
}

fn git_checked(root: &Path, args: &[&str]) -> Result<String, String> {
    let output = git_output(root, args)?;
    if output.status.success() {
        Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        Err(if stderr.is_empty() {
            format!("{} failed", command_text(args))
        } else {
            stderr
        })
    }
}

fn git_checked_with_paths(root: &Path, args: &[&str], paths: &[String]) -> Result<String, String> {
    validate_relative_paths(paths)?;
    let mut command = Command::new("git");
    command.arg("-C").arg(root).args(args).arg("--").args(paths);
    let output = command
        .output()
        .map_err(|err| format!("failed to run git {}: {err}", args.join(" ")))?;
    if output.status.success() {
        Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        Err(if stderr.is_empty() {
            format!("git {} failed", args.join(" "))
        } else {
            stderr
        })
    }
}

fn optional_git_string(root: &Path, args: &[&str]) -> Option<String> {
    git_checked(root, args)
        .ok()
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
}

fn validate_relative_paths(paths: &[String]) -> Result<(), String> {
    if paths.is_empty() {
        return Err("choose at least one path".to_string());
    }
    for path in paths {
        if path.is_empty() || path.contains('\0') {
            return Err("path must not be empty".to_string());
        }
        let parsed = Path::new(path);
        if parsed.is_absolute() {
            return Err(format!("{path} must be relative to the repository"));
        }
        if parsed.components().any(|component| {
            matches!(
                component,
                Component::ParentDir | Component::RootDir | Component::Prefix(_)
            )
        }) {
            return Err(format!("{path} escapes the repository"));
        }
    }
    Ok(())
}

fn status_token(byte: u8) -> Option<String> {
    match byte {
        b' ' => None,
        value => Some((value as char).to_string()),
    }
}

fn repo_root_for(root: &Path) -> Result<Option<PathBuf>, String> {
    let output = git_output(root, &["rev-parse", "--show-toplevel"])?;
    if !output.status.success() {
        return Ok(None);
    }
    let path = String::from_utf8_lossy(&output.stdout).trim().to_string();
    if path.is_empty() {
        Ok(None)
    } else {
        Ok(Some(PathBuf::from(path)))
    }
}

fn ahead_behind(root: &Path, upstream: Option<&str>) -> (u32, u32) {
    if upstream.is_none() {
        return (0, 0);
    }
    let Ok(output) = git_output(
        root,
        &["rev-list", "--left-right", "--count", "@{u}...HEAD"],
    ) else {
        return (0, 0);
    };
    if !output.status.success() {
        return (0, 0);
    }
    let counts = String::from_utf8_lossy(&output.stdout);
    let mut parts = counts.split_whitespace();
    let behind = parts.next().and_then(|part| part.parse().ok()).unwrap_or(0);
    let ahead = parts.next().and_then(|part| part.parse().ok()).unwrap_or(0);
    (ahead, behind)
}

fn parse_status_entries(repo_root: &Path, bytes: &[u8]) -> Vec<GitStatusEntry> {
    let records: Vec<&[u8]> = bytes
        .split(|byte| *byte == 0)
        .filter(|record| !record.is_empty())
        .collect();
    let mut entries = Vec::new();
    let mut index = 0;
    while index < records.len() {
        let record = records[index];
        if record.len() < 4 {
            index += 1;
            continue;
        }
        let path = String::from_utf8_lossy(&record[3..]).to_string();
        let index_status = status_token(record[0]);
        let worktree_status = status_token(record[1]);
        let has_original_path =
            matches!(record[0], b'R' | b'C') || matches!(record[1], b'R' | b'C');
        let original_path = if has_original_path && index + 1 < records.len() {
            index += 1;
            Some(String::from_utf8_lossy(records[index]).to_string())
        } else {
            None
        };
        entries.push(GitStatusEntry {
            full_path: repo_root.join(&path),
            path,
            original_path,
            index_status,
            worktree_status,
        });
        index += 1;
    }
    entries
}

#[tauri::command]
pub async fn git_status(root: PathBuf) -> Result<GitStatus, String> {
    let Some(repo_root) = repo_root_for(&root)? else {
        return Ok(GitStatus {
            is_repo: false,
            repo_root: None,
            branch: None,
            upstream: None,
            ahead: 0,
            behind: 0,
            entries: Vec::new(),
        });
    };

    let branch = optional_git_string(&root, &["symbolic-ref", "--short", "HEAD"])
        .or_else(|| optional_git_string(&root, &["rev-parse", "--short", "HEAD"]));
    let upstream = optional_git_string(
        &root,
        &["rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"],
    );
    let (ahead, behind) = ahead_behind(&root, upstream.as_deref());
    let status_output = git_output(
        &root,
        &["status", "--porcelain=v1", "-z", "--untracked-files=all"],
    )?;
    if !status_output.status.success() {
        return Err(String::from_utf8_lossy(&status_output.stderr)
            .trim()
            .to_string());
    }

    Ok(GitStatus {
        is_repo: true,
        repo_root: Some(repo_root.clone()),
        branch,
        upstream,
        ahead,
        behind,
        entries: parse_status_entries(&repo_root, &status_output.stdout),
    })
}

#[tauri::command]
pub async fn git_stage_paths(root: PathBuf, paths: Vec<String>) -> Result<String, String> {
    git_checked_with_paths(&root, &["add"], &paths)
}

#[tauri::command]
pub async fn git_unstage_paths(root: PathBuf, paths: Vec<String>) -> Result<String, String> {
    git_checked_with_paths(&root, &["restore", "--staged"], &paths)
}

#[tauri::command]
pub async fn git_discard_paths(root: PathBuf, paths: Vec<String>) -> Result<String, String> {
    validate_relative_paths(&paths)?;
    let status = git_status(root.clone()).await?;
    for path in paths {
        let entry = status.entries.iter().find(|entry| entry.path == path);
        let is_untracked = matches!(
            entry.and_then(|entry| entry.worktree_status.as_deref()),
            Some("?")
        );
        let is_added = matches!(
            entry.and_then(|entry| entry.index_status.as_deref()),
            Some("A")
        );
        if is_untracked {
            git_checked_with_paths(&root, &["clean", "-f"], &[path])?;
        } else if is_added {
            git_checked_with_paths(&root, &["restore", "--staged"], std::slice::from_ref(&path))?;
            git_checked_with_paths(&root, &["clean", "-f"], &[path])?;
        } else {
            git_checked_with_paths(
                &root,
                &["restore", "--source=HEAD", "--staged", "--worktree"],
                &[path],
            )?;
        }
    }
    Ok(String::new())
}

#[tauri::command]
pub async fn git_commit(root: PathBuf, message: String) -> Result<String, String> {
    let message = message.trim();
    if message.is_empty() {
        return Err("commit message is required".to_string());
    }
    let output = Command::new("git")
        .arg("-C")
        .arg(&root)
        .args(["commit", "-m", message])
        .output()
        .map_err(|err| format!("failed to run git commit: {err}"))?;
    if output.status.success() {
        Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        Err(if stderr.is_empty() {
            "git commit failed".to_string()
        } else {
            stderr
        })
    }
}

#[tauri::command]
pub async fn git_sync(root: PathBuf) -> Result<String, String> {
    let mut output = String::new();
    let pull = git_checked(&root, &["pull", "--ff-only"])?;
    if !pull.is_empty() {
        output.push_str(&pull);
        output.push('\n');
    }
    let push = git_checked(&root, &["push"])?;
    if !push.is_empty() {
        output.push_str(&push);
    }
    Ok(output.trim().to_string())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_porcelain_entries() {
        let bytes = b" M src/main.ts\0A  README.md\0?? notes.txt\0R  new.ts\0old.ts\0";
        let entries = parse_status_entries(Path::new("/repo"), bytes);

        assert_eq!(entries.len(), 4);
        assert_eq!(entries[0].path, "src/main.ts");
        assert_eq!(entries[0].worktree_status.as_deref(), Some("M"));
        assert_eq!(entries[1].index_status.as_deref(), Some("A"));
        assert_eq!(entries[2].index_status.as_deref(), Some("?"));
        assert_eq!(entries[3].original_path.as_deref(), Some("old.ts"));
        assert_eq!(entries[3].full_path, PathBuf::from("/repo/new.ts"));
    }
}
