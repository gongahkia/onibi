use serde::Serialize;
use std::{
    fs,
    path::{Component, Path, PathBuf},
    process::{Command, Output},
};

const MAX_DIFF_FILE_BYTES: u64 = 2 * 1024 * 1024;

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

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GitFileDiff {
    path: String,
    old_label: String,
    new_label: String,
    old_text: Option<String>,
    new_text: Option<String>,
    binary: bool,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(rename_all = "camelCase")]
pub struct GitWorktree {
    path: PathBuf,
    branch: Option<String>,
    head: Option<String>,
    detached: bool,
    bare: bool,
    prunable: bool,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct GitCloneResult {
    path: PathBuf,
    name: String,
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

fn git_blob(root: &Path, spec: &str) -> Result<Option<Vec<u8>>, String> {
    let output = Command::new("git")
        .arg("-C")
        .arg(root)
        .arg("cat-file")
        .arg("-e")
        .arg(spec)
        .output()
        .map_err(|err| format!("failed to run git cat-file: {err}"))?;
    if !output.status.success() {
        return Ok(None);
    }
    let output = Command::new("git")
        .arg("-C")
        .arg(root)
        .arg("show")
        .arg(spec)
        .output()
        .map_err(|err| format!("failed to run git show: {err}"))?;
    if output.status.success() {
        Ok(Some(output.stdout))
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        Err(if stderr.is_empty() {
            "git show failed".to_string()
        } else {
            stderr
        })
    }
}

fn text_from_bytes(bytes: Option<Vec<u8>>) -> (Option<String>, bool) {
    let Some(bytes) = bytes else {
        return (None, false);
    };
    if bytes.len() as u64 > MAX_DIFF_FILE_BYTES || bytes.iter().any(|byte| *byte == 0) {
        return (None, true);
    }
    match String::from_utf8(bytes) {
        Ok(text) => (Some(text), false),
        Err(_) => (None, true),
    }
}

fn read_worktree_file(repo_root: &Path, path: &str) -> Result<Option<Vec<u8>>, String> {
    let target = repo_root.join(path);
    if !target.exists() {
        return Ok(None);
    }
    let metadata = fs::metadata(&target).map_err(|err| err.to_string())?;
    if !metadata.is_file() {
        return Ok(None);
    }
    if metadata.len() > MAX_DIFF_FILE_BYTES {
        return Ok(Some(vec![0]));
    }
    fs::read(&target).map(Some).map_err(|err| err.to_string())
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

fn parse_worktrees(raw: &str) -> Vec<GitWorktree> {
    let mut worktrees = Vec::new();
    let mut current: Option<GitWorktree> = None;
    for line in raw.lines() {
        let line = line.trim_end();
        if line.is_empty() {
            if let Some(worktree) = current.take() {
                worktrees.push(worktree);
            }
            continue;
        }
        if let Some(path) = line.strip_prefix("worktree ") {
            if let Some(worktree) = current.take() {
                worktrees.push(worktree);
            }
            current = Some(GitWorktree {
                path: PathBuf::from(path),
                branch: None,
                head: None,
                detached: false,
                bare: false,
                prunable: false,
            });
            continue;
        }
        let Some(worktree) = current.as_mut() else {
            continue;
        };
        if let Some(head) = line.strip_prefix("HEAD ") {
            worktree.head = Some(head.to_string());
        } else if let Some(branch) = line.strip_prefix("branch ") {
            worktree.branch = Some(
                branch
                    .strip_prefix("refs/heads/")
                    .unwrap_or(branch)
                    .to_string(),
            );
        } else if line == "detached" {
            worktree.detached = true;
        } else if line == "bare" {
            worktree.bare = true;
        } else if line == "prunable" || line.starts_with("prunable ") {
            worktree.prunable = true;
        }
    }
    if let Some(worktree) = current {
        worktrees.push(worktree);
    }
    worktrees
}

fn validate_branch_name(branch: &str) -> Result<String, String> {
    let branch = branch.trim();
    if branch.is_empty() || branch.contains('\0') {
        return Err("branch name is required".to_string());
    }
    if branch.starts_with('-') || branch.contains("..") || branch.ends_with('/') {
        return Err("branch name is not valid for a worktree".to_string());
    }
    Ok(branch.to_string())
}

fn validate_clone_remote(remote: &str) -> Result<String, String> {
    let remote = remote.trim();
    if remote.is_empty() || remote.contains('\0') || remote.starts_with('-') {
        return Err("repository URL is not valid".to_string());
    }
    Ok(remote.to_string())
}

fn clone_name_from_remote(remote: &str) -> String {
    let trimmed = remote.trim().trim_end_matches('/').trim_end_matches(".git");
    let separator = trimmed
        .rfind(|character| character == '/' || character == ':')
        .map(|index| index + 1)
        .unwrap_or(0);
    trimmed[separator..]
        .chars()
        .map(|character| {
            if character.is_ascii_alphanumeric() || matches!(character, '.' | '_' | '-') {
                character
            } else {
                '-'
            }
        })
        .collect::<String>()
        .trim_matches('-')
        .to_string()
}

fn validate_clone_name(name: &str) -> Result<String, String> {
    let name = name.trim();
    if name.is_empty()
        || name == "."
        || name == ".."
        || name.starts_with('-')
        || name.contains('/')
        || name.contains('\\')
        || name.contains('\0')
    {
        return Err("folder name is not valid".to_string());
    }
    Ok(name.to_string())
}

fn canonical_dir(path: &Path) -> Result<PathBuf, String> {
    let resolved = fs::canonicalize(path)
        .map_err(|err| format!("failed to resolve {}: {err}", path.display()))?;
    let metadata = fs::metadata(&resolved).map_err(|err| err.to_string())?;
    if !metadata.is_dir() {
        return Err(format!("{} is not a directory", resolved.display()));
    }
    Ok(resolved)
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

#[tauri::command]
pub async fn git_diff_file(
    root: PathBuf,
    path: String,
    stage: String,
) -> Result<GitFileDiff, String> {
    validate_relative_paths(std::slice::from_ref(&path))?;
    let Some(repo_root) = repo_root_for(&root)? else {
        return Err("No Git repository in this workspace.".to_string());
    };

    let head_spec = format!("HEAD:{path}");
    let index_spec = format!(":{path}");
    let (old_label, new_label, old_bytes, new_bytes) = if stage == "staged" {
        (
            "HEAD".to_string(),
            "Index".to_string(),
            git_blob(&repo_root, &head_spec)?,
            git_blob(&repo_root, &index_spec)?,
        )
    } else {
        let index = git_blob(&repo_root, &index_spec)?;
        (
            "Index".to_string(),
            "Working tree".to_string(),
            if index.is_some() {
                index
            } else {
                git_blob(&repo_root, &head_spec)?
            },
            read_worktree_file(&repo_root, &path)?,
        )
    };

    let (old_text, old_binary) = text_from_bytes(old_bytes);
    let (new_text, new_binary) = text_from_bytes(new_bytes);
    Ok(GitFileDiff {
        path,
        old_label,
        new_label,
        old_text,
        new_text,
        binary: old_binary || new_binary,
    })
}

#[tauri::command]
pub async fn git_worktrees(root: PathBuf) -> Result<Vec<GitWorktree>, String> {
    let Some(repo_root) = repo_root_for(&root)? else {
        return Ok(Vec::new());
    };
    let output = git_checked(&repo_root, &["worktree", "list", "--porcelain"])?;
    Ok(parse_worktrees(&output))
}

#[tauri::command]
pub async fn git_create_worktree(
    root: PathBuf,
    branch: String,
    path: PathBuf,
    base: Option<String>,
) -> Result<GitWorktree, String> {
    let Some(repo_root) = repo_root_for(&root)? else {
        return Err("No Git repository in this workspace.".to_string());
    };
    let branch = validate_branch_name(&branch)?;
    let base = base
        .map(|value| value.trim().to_string())
        .filter(|value| !value.is_empty())
        .unwrap_or_else(|| "HEAD".to_string());
    let output = Command::new("git")
        .arg("-C")
        .arg(&repo_root)
        .args(["worktree", "add", "-b"])
        .arg(&branch)
        .arg(&path)
        .arg(&base)
        .output()
        .map_err(|err| format!("failed to run git worktree add: {err}"))?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        return Err(if stderr.is_empty() {
            "git worktree add failed".to_string()
        } else {
            stderr
        });
    }
    git_worktrees(repo_root)
        .await?
        .into_iter()
        .find(|worktree| worktree.path == path)
        .ok_or_else(|| "worktree was created but could not be listed".to_string())
}

#[tauri::command]
pub async fn git_remove_worktree(
    root: PathBuf,
    path: PathBuf,
    force: bool,
) -> Result<String, String> {
    let Some(repo_root) = repo_root_for(&root)? else {
        return Err("No Git repository in this workspace.".to_string());
    };
    let mut command = Command::new("git");
    command
        .arg("-C")
        .arg(&repo_root)
        .args(["worktree", "remove"]);
    if force {
        command.arg("--force");
    }
    let output = command
        .arg(path)
        .output()
        .map_err(|err| format!("failed to run git worktree remove: {err}"))?;
    if output.status.success() {
        Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
    } else {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        Err(if stderr.is_empty() {
            "git worktree remove failed".to_string()
        } else {
            stderr
        })
    }
}

#[tauri::command]
pub async fn git_clone_repository(
    remote: String,
    destination_parent: PathBuf,
    name: Option<String>,
) -> Result<GitCloneResult, String> {
    let remote = validate_clone_remote(&remote)?;
    let parent = canonical_dir(&destination_parent)?;
    let name = validate_clone_name(
        &name
            .filter(|value| !value.trim().is_empty())
            .unwrap_or_else(|| clone_name_from_remote(&remote)),
    )?;
    let target = parent.join(&name);
    if target.exists() {
        return Err(format!("{} already exists", target.display()));
    }

    let output = Command::new("git")
        .args(["clone", "--"])
        .arg(&remote)
        .arg(&target)
        .output()
        .map_err(|err| format!("failed to run git clone: {err}"))?;
    if !output.status.success() {
        let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
        return Err(if stderr.is_empty() {
            "git clone failed".to_string()
        } else {
            stderr
        });
    }

    Ok(GitCloneResult { path: target, name })
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

    #[test]
    fn parses_worktree_porcelain() {
        let raw = "\
worktree /repo
HEAD abc123
branch refs/heads/main

worktree /repo-feature
HEAD def456
branch refs/heads/feature/mobile

worktree /repo-detached
HEAD fedcba
detached
prunable gitdir file points to non-existent location
";
        let worktrees = parse_worktrees(raw);
        assert_eq!(worktrees.len(), 3);
        assert_eq!(worktrees[0].path, PathBuf::from("/repo"));
        assert_eq!(worktrees[0].branch.as_deref(), Some("main"));
        assert_eq!(worktrees[1].branch.as_deref(), Some("feature/mobile"));
        assert!(worktrees[2].detached);
        assert!(worktrees[2].prunable);
    }
}
