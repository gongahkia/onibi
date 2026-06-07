use crate::{
    protocol::{Approval, Decision},
    secret,
};
use anyhow::{Context, Result};
use regex::Regex;
use serde::{Deserialize, Serialize};
use serde_json::Value;
use std::{fs, path::PathBuf};

#[derive(Debug, Clone, Deserialize, Default)]
pub struct PolicyFile {
    #[serde(default)]
    pub policy: Vec<PolicyRule>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct PolicyRule {
    #[serde(default)]
    pub name: Option<String>,
    #[serde(rename = "match")]
    pub matcher: PolicyMatcher,
    pub decision: PolicyDecision,
    #[serde(default)]
    pub require_edit: bool,
}

#[derive(Debug, Clone, Deserialize, Default)]
pub struct PolicyMatcher {
    #[serde(default)]
    pub agent: Option<String>,
    #[serde(default)]
    pub tool: Option<String>,
    #[serde(default)]
    pub command: Option<String>,
    #[serde(default)]
    pub cwd_prefix: Option<String>,
}

#[derive(Debug, Clone, Copy, Deserialize, Serialize, PartialEq, Eq)]
#[serde(rename_all = "kebab-case")]
pub enum PolicyDecision {
    AutoAllow,
    AlwaysAsk,
    AlwaysDeny,
}

#[derive(Debug, Clone, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct PolicyValidation {
    pub path: String,
    pub exists: bool,
    pub rule_count: usize,
    pub ok: bool,
    pub error: Option<String>,
}

#[derive(Debug, Clone)]
pub struct PolicyEvaluation {
    pub rule_name: Option<String>,
    pub decision: PolicyDecision,
}

impl PolicyEvaluation {
    pub fn response_decision(&self) -> Option<Decision> {
        match self.decision {
            PolicyDecision::AutoAllow => Some(Decision::Allow),
            PolicyDecision::AlwaysDeny => Some(Decision::Deny),
            PolicyDecision::AlwaysAsk => None,
        }
    }

    pub fn reason(&self) -> String {
        let rule = self.rule_name.as_deref().unwrap_or("unnamed policy");
        match self.decision {
            PolicyDecision::AutoAllow => format!("auto-allowed by policy: {rule}"),
            PolicyDecision::AlwaysDeny => format!("denied by policy: {rule}"),
            PolicyDecision::AlwaysAsk => format!("manual approval required by policy: {rule}"),
        }
    }
}

pub fn path() -> Result<PathBuf> {
    Ok(secret::config_dir()?.join("policies.toml"))
}

pub fn validate() -> PolicyValidation {
    let path = match path() {
        Ok(path) => path,
        Err(error) => {
            return PolicyValidation {
                path: "~/.config/onibi/policies.toml".to_string(),
                exists: false,
                rule_count: 0,
                ok: false,
                error: Some(error.to_string()),
            };
        }
    };
    match load_from_path(&path) {
        Ok(file) => PolicyValidation {
            path: path.display().to_string(),
            exists: path.exists(),
            rule_count: file.policy.len(),
            ok: true,
            error: None,
        },
        Err(error) => PolicyValidation {
            path: path.display().to_string(),
            exists: path.exists(),
            rule_count: 0,
            ok: false,
            error: Some(error.to_string()),
        },
    }
}

pub fn evaluate(approval: &Approval) -> Result<Option<PolicyEvaluation>> {
    let file = load()?;
    evaluate_rules(file, approval)
}

fn evaluate_rules(file: PolicyFile, approval: &Approval) -> Result<Option<PolicyEvaluation>> {
    for rule in file.policy {
        if rule.matches(approval)? {
            if rule.require_edit && matches!(rule.decision, PolicyDecision::AutoAllow) {
                return Ok(Some(PolicyEvaluation {
                    rule_name: rule.name,
                    decision: PolicyDecision::AlwaysAsk,
                }));
            }
            return Ok(Some(PolicyEvaluation {
                rule_name: rule.name,
                decision: rule.decision,
            }));
        }
    }
    Ok(None)
}

fn load() -> Result<PolicyFile> {
    let path = path()?;
    load_from_path(&path)
}

fn load_from_path(path: &PathBuf) -> Result<PolicyFile> {
    if !path.exists() {
        return Ok(PolicyFile::default());
    }
    let raw = fs::read_to_string(path).with_context(|| format!("read {}", path.display()))?;
    let file: PolicyFile =
        toml::from_str(&raw).with_context(|| format!("parse {}", path.display()))?;
    for rule in &file.policy {
        rule.validate()?;
    }
    Ok(file)
}

impl PolicyRule {
    fn validate(&self) -> Result<()> {
        if let Some(pattern) = &self.matcher.command {
            Regex::new(pattern).with_context(|| {
                format!(
                    "invalid command regex in policy {}",
                    self.name.as_deref().unwrap_or("<unnamed>")
                )
            })?;
        }
        Ok(())
    }

    fn matches(&self, approval: &Approval) -> Result<bool> {
        if !matches_string(self.matcher.agent.as_deref(), &approval.agent) {
            return Ok(false);
        }
        if !matches_string(self.matcher.tool.as_deref(), &approval.tool) {
            return Ok(false);
        }
        if let Some(prefix) = &self.matcher.cwd_prefix {
            if !approval.cwd.starts_with(prefix) {
                return Ok(false);
            }
        }
        if let Some(pattern) = &self.matcher.command {
            let command = approval
                .input
                .get("command")
                .and_then(|value| value.as_str())
                .unwrap_or("");
            if !Regex::new(pattern)?.is_match(command) {
                return Ok(false);
            }
        }
        Ok(true)
    }
}

fn matches_string(expected: Option<&str>, actual: &str) -> bool {
    expected.map(|expected| expected == actual).unwrap_or(true)
}

pub fn evaluate_safe_mode(approval: &Approval) -> PolicyEvaluation {
    if safe_read_only_bash(approval) {
        PolicyEvaluation {
            rule_name: Some("safe mode read-only basics".to_string()),
            decision: PolicyDecision::AutoAllow,
        }
    } else {
        PolicyEvaluation {
            rule_name: Some("safe mode default ask".to_string()),
            decision: PolicyDecision::AlwaysAsk,
        }
    }
}

fn safe_read_only_bash(approval: &Approval) -> bool {
    if approval.tool != "Bash" {
        return false;
    }
    let Some(command) = approval.input.get("command").and_then(Value::as_str) else {
        return false;
    };
    let command = command.trim();
    if command.is_empty() || contains_shell_control(command) {
        return false;
    }
    let parts: Vec<&str> = command.split_whitespace().collect();
    match parts.as_slice() {
        ["pwd"] => true,
        ["ls", ..] => true,
        ["cat", ..] => true,
        ["head", ..] => true,
        ["tail", ..] => true,
        ["grep", ..] => true,
        ["rg", ..] => true,
        ["sed", "-n", ..] => true,
        ["find", args @ ..] => !args.iter().any(|arg| {
            matches!(
                *arg,
                "-delete" | "-exec" | "-execdir" | "-ok" | "-okdir" | "-fdelete"
            )
        }),
        ["git", "status", ..]
        | ["git", "diff", ..]
        | ["git", "log", ..]
        | ["git", "show", ..] => true,
        _ => false,
    }
}

fn contains_shell_control(command: &str) -> bool {
    ["|", ">", "<", ";", "&&", "||", "`", "$(", "\n", "\r"]
        .iter()
        .any(|token| command.contains(token))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::protocol::PROTOCOL_VERSION;
    use serde_json::json;
    use tempfile::tempdir;

    fn approval(command: &str) -> Approval {
        Approval {
            protocol_version: PROTOCOL_VERSION.to_string(),
            approval_id: "approval".to_string(),
            machine_id: "machine".to_string(),
            session_id: "session".to_string(),
            agent: "claude-code".to_string(),
            tool: "Bash".to_string(),
            input: json!({ "command": command }),
            cwd: "/repo".to_string(),
            metadata: None,
            decision: None,
            updated_input: None,
            reason: None,
            decided_by: None,
            created_at: 1,
            decided_at: None,
        }
    }

    #[test]
    fn evaluates_first_matching_rule() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("policies.toml");
        fs::write(
            &path,
            r#"
            [[policy]]
            name = "safe git"
            decision = "auto-allow"
            [policy.match]
            tool = "Bash"
            command = "^git (status|diff)"
            "#,
        )
        .unwrap();

        let file = load_from_path(&path).unwrap();
        let result = evaluate_rules(file, &approval("git status"))
            .unwrap()
            .unwrap();
        assert_eq!(result.decision, PolicyDecision::AutoAllow);
        assert_eq!(result.response_decision(), Some(Decision::Allow));
    }

    #[test]
    fn require_edit_forces_manual_approval() {
        let dir = tempdir().unwrap();
        let path = dir.path().join("policies.toml");
        fs::write(
            &path,
            r#"
            [[policy]]
            name = "risky"
            decision = "auto-allow"
            require_edit = true
            [policy.match]
            command = "^rm "
            "#,
        )
        .unwrap();

        let file = load_from_path(&path).unwrap();
        let result = evaluate_rules(file, &approval("rm -rf tmp"))
            .unwrap()
            .unwrap();
        assert_eq!(result.decision, PolicyDecision::AlwaysAsk);
        assert_eq!(result.response_decision(), None);
    }

    #[test]
    fn safe_mode_auto_allows_read_only_basics() {
        let result = evaluate_safe_mode(&approval("git status --short"));
        assert_eq!(result.decision, PolicyDecision::AutoAllow);
        assert_eq!(result.response_decision(), Some(Decision::Allow));
    }

    #[test]
    fn safe_mode_asks_for_shell_control_or_unknown_commands() {
        assert_eq!(
            evaluate_safe_mode(&approval("git status && rm -rf tmp")).decision,
            PolicyDecision::AlwaysAsk
        );
        assert_eq!(
            evaluate_safe_mode(&approval("python script.py")).decision,
            PolicyDecision::AlwaysAsk
        );
    }
}
