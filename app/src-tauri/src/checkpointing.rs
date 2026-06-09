use crate::{
    approval::store::now_millis,
    protocol::{Approval, CheckpointDiff, CheckpointDiffFile, CheckpointRecord, PROTOCOL_VERSION},
};
use anyhow::{anyhow, bail, Context, Result};
use std::{
    fs,
    path::{Path, PathBuf},
    process::{Command, Output},
};
use ulid::Ulid;

const MAX_DIFF_FILE_BYTES: u64 = 2 * 1024 * 1024;

#[derive(Debug, Clone)]
pub struct CheckpointGuardrails {
    pub max_changed_files: usize,
    pub max_index_bytes: u64,
    pub max_file_bytes: u64,
    pub ignored_path_globs: Vec<String>,
}

impl Default for CheckpointGuardrails {
    fn default() -> Self {
        Self {
            max_changed_files: crate::config::DEFAULT_CHECKPOINT_MAX_CHANGED_FILES,
            max_index_bytes: crate::config::DEFAULT_CHECKPOINT_MAX_INDEX_BYTES,
            max_file_bytes: crate::config::DEFAULT_CHECKPOINT_MAX_FILE_BYTES,
            ignored_path_globs: Vec::new(),
        }
    }
}

#[derive(Debug, Clone, Copy, Default)]
pub struct CheckpointPruneRefSummary {
    pub refs_attempted: usize,
    pub refs_deleted: usize,
    pub refs_failed: usize,
}

pub fn pre_ref(approval_id: &str) -> String {
    format!("refs/onibi/turns/{}/pre", ref_component(approval_id))
}

pub fn post_ref(approval_id: &str) -> String {
    format!("refs/onibi/turns/{}/post", ref_component(approval_id))
}

pub fn skipped_pre_record(approval: &Approval, error: impl Into<String>) -> CheckpointRecord {
    let now = now_millis();
    CheckpointRecord {
        approval_id: approval.approval_id.clone(),
        session_id: approval.session_id.clone(),
        cwd: approval.cwd.clone(),
        pre_ref: pre_ref(&approval.approval_id),
        post_ref: None,
        created_at: now,
        updated_at: now,
        error: Some(error.into()),
    }
}

pub fn snapshot_pre(
    approval: &Approval,
    guardrails: &CheckpointGuardrails,
) -> Result<CheckpointRecord> {
    let cwd = PathBuf::from(&approval.cwd);
    let pre_ref = pre_ref(&approval.approval_id);
    snapshot_ref(
        &cwd,
        &pre_ref,
        &format!("onibi pre {}", approval.approval_id),
        guardrails,
    )?;
    let now = now_millis();
    Ok(CheckpointRecord {
        approval_id: approval.approval_id.clone(),
        session_id: approval.session_id.clone(),
        cwd: approval.cwd.clone(),
        pre_ref,
        post_ref: None,
        created_at: now,
        updated_at: now,
        error: None,
    })
}

pub fn snapshot_post(
    record: &CheckpointRecord,
    guardrails: &CheckpointGuardrails,
) -> Result<String> {
    let post_ref = post_ref(&record.approval_id);
    snapshot_ref(
        Path::new(&record.cwd),
        &post_ref,
        &format!("onibi post {}", record.approval_id),
        guardrails,
    )?;
    Ok(post_ref)
}

pub fn diff(record: &CheckpointRecord) -> Result<CheckpointDiff> {
    let Some(post_ref) = record.post_ref.clone() else {
        return Ok(CheckpointDiff {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id: record.approval_id.clone(),
            pre_ref: record.pre_ref.clone(),
            post_ref: None,
            files: Vec::new(),
        });
    };
    let root = repo_root_for(Path::new(&record.cwd))?;
    let output = git_output(
        &root,
        &["diff", "--name-only", "-z", &record.pre_ref, &post_ref],
    )?;
    if !output.status.success() {
        return Err(git_stderr(output, "git diff failed"));
    }
    let files = output
        .stdout
        .split(|byte| *byte == 0)
        .filter(|path| !path.is_empty())
        .map(|path| String::from_utf8_lossy(path).to_string())
        .map(|path| diff_file(&root, &record.pre_ref, &post_ref, path))
        .collect::<Result<Vec<_>>>()?;
    Ok(CheckpointDiff {
        protocol_version: PROTOCOL_VERSION.to_string(),
        approval_id: record.approval_id.clone(),
        pre_ref: record.pre_ref.clone(),
        post_ref: Some(post_ref),
        files,
    })
}

pub fn restore(record: &CheckpointRecord) -> Result<()> {
    let root = repo_root_for(Path::new(&record.cwd))?;
    git_checked(&root, &["rev-parse", "--verify", &record.pre_ref])?;
    git_checked(&root, &["reset", "--hard", &record.pre_ref])?;
    git_checked(&root, &["clean", "-fd"])?;
    Ok(())
}

pub fn prune_refs(records: &[CheckpointRecord]) -> CheckpointPruneRefSummary {
    let mut summary = CheckpointPruneRefSummary::default();
    for record in records {
        prune_ref(&mut summary, &record.cwd, &record.pre_ref);
        if let Some(post_ref) = record.post_ref.as_deref() {
            prune_ref(&mut summary, &record.cwd, post_ref);
        }
    }
    summary
}

fn prune_ref(summary: &mut CheckpointPruneRefSummary, cwd: &str, reference: &str) {
    summary.refs_attempted += 1;
    match delete_ref(Path::new(cwd), reference) {
        Ok(RefDeleteOutcome::Deleted) => summary.refs_deleted += 1,
        Ok(RefDeleteOutcome::Missing) => {}
        Err(error) => {
            summary.refs_failed += 1;
            tracing::debug!(%error, %reference, "checkpoint ref prune failed");
        }
    }
}

enum RefDeleteOutcome {
    Deleted,
    Missing,
}

fn delete_ref(cwd: &Path, reference: &str) -> Result<RefDeleteOutcome> {
    let root = repo_root_for(cwd)?;
    let verify = git_output(&root, &["rev-parse", "--verify", reference])?;
    if !verify.status.success() {
        return Ok(RefDeleteOutcome::Missing);
    }
    let output = git_output(&root, &["update-ref", "-d", reference])?;
    if output.status.success() {
        Ok(RefDeleteOutcome::Deleted)
    } else {
        Err(git_stderr(output, "git update-ref -d failed"))
    }
}

fn snapshot_ref(
    cwd: &Path,
    reference: &str,
    message: &str,
    guardrails: &CheckpointGuardrails,
) -> Result<String> {
    let root = repo_root_for(cwd)?;
    guard_snapshot_size(&root, guardrails)?;
    let index_path = std::env::temp_dir().join(format!(
        "onibi-checkpoint-{}.index",
        Ulid::new().to_string().to_ascii_lowercase()
    ));
    let result = snapshot_ref_with_index(&root, reference, message, &index_path, guardrails);
    let _ = fs::remove_file(index_path);
    result
}

fn snapshot_ref_with_index(
    root: &Path,
    reference: &str,
    message: &str,
    index_path: &Path,
    guardrails: &CheckpointGuardrails,
) -> Result<String> {
    if git_checked(root, &["rev-parse", "--verify", "HEAD"]).is_ok() {
        git_checked_env(root, &["read-tree", "HEAD"], index_path)?;
    } else {
        git_checked_env(root, &["read-tree", "--empty"], index_path)?;
    }
    let mut add_args = vec![
        "add".to_string(),
        "-A".to_string(),
        "--".to_string(),
        ".".to_string(),
    ];
    add_args.extend(exclude_pathspecs(guardrails));
    git_checked_env_owned(root, &add_args, index_path)?;
    let tree = git_checked_env(root, &["write-tree"], index_path)?;
    let commit = commit_tree(root, tree.trim(), message)?;
    git_checked(root, &["update-ref", reference, commit.trim()])?;
    Ok(commit.trim().to_string())
}

fn guard_snapshot_size(root: &Path, guardrails: &CheckpointGuardrails) -> Result<()> {
    let mut args = vec![
        "status".to_string(),
        "--porcelain=v1".to_string(),
        "-z".to_string(),
        "--untracked-files=all".to_string(),
        "--".to_string(),
        ".".to_string(),
    ];
    args.extend(exclude_pathspecs(guardrails));
    let output = git_output_owned(root, &args)?;
    if !output.status.success() {
        return Err(git_stderr(output, "git status failed"));
    }
    let paths = changed_paths_from_status(&output.stdout);
    if paths.len() > guardrails.max_changed_files {
        bail!(
            "checkpoint skipped: {} changed files exceeds limit {}",
            paths.len(),
            guardrails.max_changed_files
        );
    }
    let mut total = 0u64;
    for path in paths {
        let target = root.join(&path);
        let Ok(metadata) = fs::metadata(&target) else {
            continue;
        };
        if !metadata.is_file() {
            continue;
        }
        let size = metadata.len();
        if size > guardrails.max_file_bytes {
            bail!(
                "checkpoint skipped: {path} is {} bytes, above limit {}",
                size,
                guardrails.max_file_bytes
            );
        }
        total = total.saturating_add(size);
        if total > guardrails.max_index_bytes {
            bail!(
                "checkpoint skipped: changed files total {} bytes, above limit {}",
                total,
                guardrails.max_index_bytes
            );
        }
    }
    Ok(())
}

fn changed_paths_from_status(bytes: &[u8]) -> Vec<String> {
    let mut paths = Vec::new();
    let mut index = 0;
    while index < bytes.len() {
        let Some(end) = bytes[index..].iter().position(|byte| *byte == 0) else {
            break;
        };
        let entry = &bytes[index..index + end];
        index += end + 1;
        if entry.len() < 4 {
            continue;
        }
        let status = &entry[..2];
        let path = String::from_utf8_lossy(&entry[3..]).to_string();
        paths.push(path);
        if matches!(status.first(), Some(b'R' | b'C')) {
            if let Some(old_end) = bytes[index..].iter().position(|byte| *byte == 0) {
                index += old_end + 1;
            }
        }
    }
    paths
}

fn exclude_pathspecs(guardrails: &CheckpointGuardrails) -> Vec<String> {
    guardrails
        .ignored_path_globs
        .iter()
        .map(|value| value.trim())
        .filter(|value| !value.is_empty() && !value.contains('\0'))
        .map(|value| format!(":(exclude){value}"))
        .collect()
}

fn commit_tree(root: &Path, tree: &str, message: &str) -> Result<String> {
    let mut command = Command::new("git");
    command
        .arg("-C")
        .arg(root)
        .args(["commit-tree", tree, "-m", message]);
    if git_checked(root, &["rev-parse", "--verify", "HEAD"]).is_ok() {
        command.args(["-p", "HEAD"]);
    }
    let output = command.output().context("run git commit-tree")?;
    if output.status.success() {
        Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
    } else {
        Err(git_stderr(output, "git commit-tree failed"))
    }
}

fn diff_file(
    root: &Path,
    pre_ref: &str,
    post_ref: &str,
    path: String,
) -> Result<CheckpointDiffFile> {
    let (old_text, old_binary) = text_from_bytes(git_blob(root, pre_ref, &path)?);
    let (new_text, new_binary) = text_from_bytes(git_blob(root, post_ref, &path)?);
    Ok(CheckpointDiffFile {
        path,
        old_label: "Before turn".to_string(),
        new_label: "After turn".to_string(),
        old_text,
        new_text,
        binary: old_binary || new_binary,
    })
}

fn git_blob(root: &Path, reference: &str, path: &str) -> Result<Option<Vec<u8>>> {
    let spec = format!("{reference}:{path}");
    let exists = git_output(root, &["cat-file", "-e", &spec])?;
    if !exists.status.success() {
        return Ok(None);
    }
    let output = git_output(root, &["show", &spec])?;
    if output.status.success() {
        Ok(Some(output.stdout))
    } else {
        Err(git_stderr(output, "git show failed"))
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

fn repo_root_for(cwd: &Path) -> Result<PathBuf> {
    let output = git_output(cwd, &["rev-parse", "--show-toplevel"])
        .with_context(|| format!("run git in {}", cwd.display()))?;
    if !output.status.success() {
        bail!("checkpointing requires a Git repository");
    }
    Ok(PathBuf::from(
        String::from_utf8_lossy(&output.stdout).trim().to_string(),
    ))
}

fn git_output(root: &Path, args: &[&str]) -> Result<Output> {
    Command::new("git")
        .arg("-C")
        .arg(root)
        .args(args)
        .output()
        .with_context(|| format!("run git {}", args.join(" ")))
}

fn git_output_owned(root: &Path, args: &[String]) -> Result<Output> {
    Command::new("git")
        .arg("-C")
        .arg(root)
        .args(args)
        .output()
        .with_context(|| format!("run git {}", args.join(" ")))
}

fn git_checked(root: &Path, args: &[&str]) -> Result<String> {
    let output = git_output(root, args)?;
    if output.status.success() {
        Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
    } else {
        Err(git_stderr(
            output,
            &format!("git {} failed", args.join(" ")),
        ))
    }
}

fn git_checked_env(root: &Path, args: &[&str], index_path: &Path) -> Result<String> {
    let output = Command::new("git")
        .arg("-C")
        .arg(root)
        .args(args)
        .env("GIT_INDEX_FILE", index_path)
        .output()
        .with_context(|| format!("run git {}", args.join(" ")))?;
    if output.status.success() {
        Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
    } else {
        Err(git_stderr(
            output,
            &format!("git {} failed", args.join(" ")),
        ))
    }
}

fn git_checked_env_owned(root: &Path, args: &[String], index_path: &Path) -> Result<String> {
    let output = Command::new("git")
        .arg("-C")
        .arg(root)
        .args(args)
        .env("GIT_INDEX_FILE", index_path)
        .output()
        .with_context(|| format!("run git {}", args.join(" ")))?;
    if output.status.success() {
        Ok(String::from_utf8_lossy(&output.stdout).trim().to_string())
    } else {
        Err(git_stderr(
            output,
            &format!("git {} failed", args.join(" ")),
        ))
    }
}

fn git_stderr(output: Output, fallback: &str) -> anyhow::Error {
    let stderr = String::from_utf8_lossy(&output.stderr).trim().to_string();
    anyhow!(if stderr.is_empty() {
        fallback.to_string()
    } else {
        stderr
    })
}

fn ref_component(raw: &str) -> String {
    raw.chars()
        .map(|character| {
            if character.is_ascii_alphanumeric() || matches!(character, '-' | '_' | '.') {
                character.to_ascii_lowercase()
            } else {
                '-'
            }
        })
        .collect::<String>()
        .trim_matches(|character| matches!(character, '-' | '.' | '/'))
        .to_string()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn run_git(root: &Path, args: &[&str]) {
        let output = Command::new("git")
            .arg("-C")
            .arg(root)
            .args(args)
            .output()
            .unwrap();
        assert!(
            output.status.success(),
            "git {} failed: {}",
            args.join(" "),
            String::from_utf8_lossy(&output.stderr)
        );
    }

    fn test_approval(cwd: &Path, approval_id: &str) -> Approval {
        Approval {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id: approval_id.to_string(),
            machine_id: "machine".to_string(),
            session_id: "session".to_string(),
            agent: "agent".to_string(),
            tool: "Bash".to_string(),
            input: serde_json::json!({"command": "echo hi"}),
            cwd: cwd.display().to_string(),
            metadata: None,
            decision: None,
            updated_input: None,
            reason: None,
            decided_by: None,
            created_at: 1,
            decided_at: None,
        }
    }

    fn git_repo() -> tempfile::TempDir {
        let dir = tempfile::tempdir().unwrap();
        run_git(dir.path(), &["init"]);
        run_git(dir.path(), &["config", "user.email", "test@example.com"]);
        run_git(dir.path(), &["config", "user.name", "Onibi Test"]);
        dir
    }

    #[test]
    fn refs_use_valid_sibling_layout() {
        assert_eq!(pre_ref("01H_APPROVAL"), "refs/onibi/turns/01h_approval/pre");
        assert_eq!(
            post_ref("01H_APPROVAL"),
            "refs/onibi/turns/01h_approval/post"
        );
    }

    #[test]
    fn non_repo_snapshot_fails() {
        let dir = std::env::temp_dir().join(format!("onibi-no-repo-{}", Ulid::new()));
        fs::create_dir_all(&dir).unwrap();
        let approval = Approval {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id: "approval-1".to_string(),
            machine_id: "machine".to_string(),
            session_id: "session".to_string(),
            agent: "agent".to_string(),
            tool: "Bash".to_string(),
            input: serde_json::json!({"command": "echo hi"}),
            cwd: dir.display().to_string(),
            metadata: None,
            decision: None,
            updated_input: None,
            reason: None,
            decided_by: None,
            created_at: 1,
            decided_at: None,
        };
        assert!(snapshot_pre(&approval, &CheckpointGuardrails::default()).is_err());
        let _ = fs::remove_dir_all(dir);
    }

    #[test]
    fn guardrails_reject_too_many_changed_files() {
        let dir = git_repo();
        fs::write(dir.path().join("one.txt"), "one").unwrap();
        fs::write(dir.path().join("two.txt"), "two").unwrap();
        let mut guardrails = CheckpointGuardrails::default();
        guardrails.max_changed_files = 1;

        let error = snapshot_pre(&test_approval(dir.path(), "approval-many"), &guardrails)
            .unwrap_err()
            .to_string();

        assert!(error.contains("changed files exceeds limit"));
    }

    #[test]
    fn guardrails_reject_oversized_file() {
        let dir = git_repo();
        fs::write(dir.path().join("large.txt"), "large").unwrap();
        let mut guardrails = CheckpointGuardrails::default();
        guardrails.max_file_bytes = 4;

        let error = snapshot_pre(&test_approval(dir.path(), "approval-large"), &guardrails)
            .unwrap_err()
            .to_string();

        assert!(error.contains("large.txt"));
        assert!(error.contains("above limit"));
    }

    #[test]
    fn ignored_path_globs_are_not_snapshotted() {
        let dir = git_repo();
        fs::create_dir_all(dir.path().join("dist")).unwrap();
        fs::write(dir.path().join("dist/bundle.js"), "bundle").unwrap();
        fs::write(dir.path().join("src.txt"), "src").unwrap();
        let guardrails = CheckpointGuardrails {
            ignored_path_globs: vec!["dist/**".to_string()],
            ..CheckpointGuardrails::default()
        };

        let record =
            snapshot_pre(&test_approval(dir.path(), "approval-ignore"), &guardrails).unwrap();

        run_git(
            dir.path(),
            &["cat-file", "-e", &format!("{}:src.txt", record.pre_ref)],
        );
        let missing = Command::new("git")
            .arg("-C")
            .arg(dir.path())
            .args([
                "cat-file",
                "-e",
                &format!("{}:dist/bundle.js", record.pre_ref),
            ])
            .output()
            .unwrap();
        assert!(!missing.status.success());
    }

    #[test]
    fn small_repo_snapshot_succeeds() {
        let dir = git_repo();
        fs::write(dir.path().join("small.txt"), "small").unwrap();

        let record = snapshot_pre(
            &test_approval(dir.path(), "approval-small"),
            &CheckpointGuardrails::default(),
        )
        .unwrap();

        run_git(dir.path(), &["rev-parse", "--verify", &record.pre_ref]);
        assert!(record.error.is_none());
    }
}
