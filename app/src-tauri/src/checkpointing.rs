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

pub fn pre_ref(approval_id: &str) -> String {
    format!("refs/onibi/turns/{}/pre", ref_component(approval_id))
}

pub fn post_ref(approval_id: &str) -> String {
    format!("refs/onibi/turns/{}/post", ref_component(approval_id))
}

pub fn snapshot_pre(approval: &Approval) -> Result<CheckpointRecord> {
    let cwd = PathBuf::from(&approval.cwd);
    let pre_ref = pre_ref(&approval.approval_id);
    snapshot_ref(
        &cwd,
        &pre_ref,
        &format!("onibi pre {}", approval.approval_id),
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

pub fn snapshot_post(record: &CheckpointRecord) -> Result<String> {
    let post_ref = post_ref(&record.approval_id);
    snapshot_ref(
        Path::new(&record.cwd),
        &post_ref,
        &format!("onibi post {}", record.approval_id),
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

fn snapshot_ref(cwd: &Path, reference: &str, message: &str) -> Result<String> {
    let root = repo_root_for(cwd)?;
    let index_path = std::env::temp_dir().join(format!(
        "onibi-checkpoint-{}.index",
        Ulid::new().to_string().to_ascii_lowercase()
    ));
    let result = snapshot_ref_with_index(&root, reference, message, &index_path);
    let _ = fs::remove_file(index_path);
    result
}

fn snapshot_ref_with_index(
    root: &Path,
    reference: &str,
    message: &str,
    index_path: &Path,
) -> Result<String> {
    if git_checked(root, &["rev-parse", "--verify", "HEAD"]).is_ok() {
        git_checked_env(root, &["read-tree", "HEAD"], index_path)?;
    } else {
        git_checked_env(root, &["read-tree", "--empty"], index_path)?;
    }
    git_checked_env(root, &["add", "-A", "--", "."], index_path)?;
    let tree = git_checked_env(root, &["write-tree"], index_path)?;
    let commit = commit_tree(root, tree.trim(), message)?;
    git_checked(root, &["update-ref", reference, commit.trim()])?;
    Ok(commit.trim().to_string())
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
        assert!(snapshot_pre(&approval).is_err());
        let _ = fs::remove_dir_all(dir);
    }
}
