use std::fmt::{Display, Formatter};
use std::str::FromStr;

use serde::Serialize;

pub const APPSEC_AGENT_BASELINE_PACK_ID: &str = "appsec-agent-baseline";
pub const APPSEC_AGENT_BASELINE_VERSION: &str = "1.0.0";

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
#[serde(rename_all = "kebab-case")]
pub enum PiPolicyAction {
    Allow,
    RequireApproval,
    Deny,
    LogOnly,
}

#[derive(Debug, Clone, Copy, Serialize, PartialEq, Eq)]
pub struct PiPolicyRule {
    pub id: &'static str,
    pub action: PiPolicyAction,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub approver_role: Option<&'static str>,
}

#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct PiPolicyDecision {
    pub action: PiPolicyAction,
    pub matched_rule_ids: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub approver_role: Option<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum PiPolicyVocabularyError {
    UnknownAction(String),
    UnknownRuleId(String),
}

pub const APPSEC_AGENT_BASELINE_RULES: &[PiPolicyRule] = &[
    PiPolicyRule {
        id: "appsec-agent-deny-destructive-shell",
        action: PiPolicyAction::Deny,
        approver_role: None,
    },
    PiPolicyRule {
        id: "appsec-agent-deny-secret-exfil",
        action: PiPolicyAction::Deny,
        approver_role: None,
    },
    PiPolicyRule {
        id: "appsec-agent-deny-exploit-execution",
        action: PiPolicyAction::Deny,
        approver_role: None,
    },
    PiPolicyRule {
        id: "appsec-agent-deny-persistence-lateral",
        action: PiPolicyAction::Deny,
        approver_role: None,
    },
    PiPolicyRule {
        id: "appsec-agent-review-active-scanner",
        action: PiPolicyAction::RequireApproval,
        approver_role: Some("appsec-reviewer"),
    },
    PiPolicyRule {
        id: "appsec-agent-review-container-run",
        action: PiPolicyAction::RequireApproval,
        approver_role: Some("appsec-reviewer"),
    },
    PiPolicyRule {
        id: "appsec-agent-log-docker-build",
        action: PiPolicyAction::LogOnly,
        approver_role: None,
    },
];

impl PiPolicyAction {
    pub fn as_str(self) -> &'static str {
        match self {
            PiPolicyAction::Allow => "allow",
            PiPolicyAction::RequireApproval => "require-approval",
            PiPolicyAction::Deny => "deny",
            PiPolicyAction::LogOnly => "log-only",
        }
    }
}

impl FromStr for PiPolicyAction {
    type Err = PiPolicyVocabularyError;

    fn from_str(value: &str) -> Result<Self, Self::Err> {
        match value {
            "allow" => Ok(PiPolicyAction::Allow),
            "require-approval" => Ok(PiPolicyAction::RequireApproval),
            "deny" => Ok(PiPolicyAction::Deny),
            "log-only" => Ok(PiPolicyAction::LogOnly),
            other => Err(PiPolicyVocabularyError::UnknownAction(other.to_string())),
        }
    }
}

impl Display for PiPolicyAction {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        formatter.write_str(self.as_str())
    }
}

impl Display for PiPolicyVocabularyError {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            PiPolicyVocabularyError::UnknownAction(action) => {
                write!(formatter, "unknown policy action: {action}")
            }
            PiPolicyVocabularyError::UnknownRuleId(rule_id) => {
                write!(formatter, "unknown policy rule id: {rule_id}")
            }
        }
    }
}

impl std::error::Error for PiPolicyVocabularyError {}

pub fn appsec_agent_baseline_rule(id: &str) -> Option<&'static PiPolicyRule> {
    APPSEC_AGENT_BASELINE_RULES
        .iter()
        .find(|rule| rule.id == id)
}

pub fn recognize_appsec_agent_baseline_decision(
    action: &str,
    matched_rule_ids: &[&str],
) -> Result<PiPolicyDecision, PiPolicyVocabularyError> {
    let action = PiPolicyAction::from_str(action)?;
    let mut approver_role = None;

    for rule_id in matched_rule_ids {
        let Some(rule) = appsec_agent_baseline_rule(rule_id) else {
            return Err(PiPolicyVocabularyError::UnknownRuleId(
                (*rule_id).to_string(),
            ));
        };
        if approver_role.is_none() {
            approver_role = rule.approver_role.map(str::to_string);
        }
    }

    Ok(PiPolicyDecision {
        action,
        matched_rule_ids: matched_rule_ids
            .iter()
            .map(|rule_id| (*rule_id).to_string())
            .collect(),
        approver_role,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn policy_actions_match_typescript_vocabulary() {
        assert_eq!(PiPolicyAction::from_str("allow"), Ok(PiPolicyAction::Allow));
        assert_eq!(PiPolicyAction::from_str("deny"), Ok(PiPolicyAction::Deny));
        assert_eq!(
            PiPolicyAction::from_str("require-approval"),
            Ok(PiPolicyAction::RequireApproval)
        );
        assert_eq!(
            PiPolicyAction::from_str("log-only"),
            Ok(PiPolicyAction::LogOnly)
        );
        assert!(PiPolicyAction::from_str("approve").is_err());
    }

    #[test]
    fn appsec_agent_baseline_rule_ids_match_typescript_pack() {
        let rules: Vec<(&str, PiPolicyAction, Option<&str>)> = APPSEC_AGENT_BASELINE_RULES
            .iter()
            .map(|rule| (rule.id, rule.action, rule.approver_role))
            .collect();

        assert_eq!(
            rules,
            vec![
                (
                    "appsec-agent-deny-destructive-shell",
                    PiPolicyAction::Deny,
                    None,
                ),
                ("appsec-agent-deny-secret-exfil", PiPolicyAction::Deny, None),
                (
                    "appsec-agent-deny-exploit-execution",
                    PiPolicyAction::Deny,
                    None,
                ),
                (
                    "appsec-agent-deny-persistence-lateral",
                    PiPolicyAction::Deny,
                    None,
                ),
                (
                    "appsec-agent-review-active-scanner",
                    PiPolicyAction::RequireApproval,
                    Some("appsec-reviewer"),
                ),
                (
                    "appsec-agent-review-container-run",
                    PiPolicyAction::RequireApproval,
                    Some("appsec-reviewer"),
                ),
                (
                    "appsec-agent-log-docker-build",
                    PiPolicyAction::LogOnly,
                    None,
                ),
            ]
        );
    }

    #[test]
    fn appsec_agent_baseline_decisions_validate_known_rule_ids() {
        let decision = recognize_appsec_agent_baseline_decision(
            "require-approval",
            &["appsec-agent-review-active-scanner"],
        )
        .expect("recognize decision");

        assert_eq!(decision.action, PiPolicyAction::RequireApproval);
        assert_eq!(
            decision.matched_rule_ids,
            vec!["appsec-agent-review-active-scanner".to_string()]
        );
        assert_eq!(decision.approver_role, Some("appsec-reviewer".to_string()));
        assert!(recognize_appsec_agent_baseline_decision("deny", &["unknown"]).is_err());
    }
}
