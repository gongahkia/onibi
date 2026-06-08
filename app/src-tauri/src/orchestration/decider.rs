use super::{canonical_agent, contains_any_phrase, normalize_detection_text, strip_ansi, AgentStatus};

pub(super) fn infer_status_from_output(agent: Option<&str>, text: &str) -> Option<AgentStatus> {
    let command_start = text.rfind("\u{1b}]133;C");
    let command_done = text
        .rfind("\u{1b}]133;D")
        .into_iter()
        .chain(text.rfind("\u{1b}]133;A"))
        .max();
    let shell_status = match (command_start, command_done) {
        (Some(start), Some(done)) if done > start => Some(AgentStatus::Idle),
        (Some(_), _) => Some(AgentStatus::Working),
        (_, Some(_)) => Some(AgentStatus::Idle),
        _ => None,
    };
    if shell_status.is_some() {
        return shell_status;
    }

    let Some(agent) = agent.and_then(canonical_agent) else {
        return None;
    };
    if agent == "shell" {
        return None;
    }

    let normalized = normalize_detection_text(&strip_ansi(text));
    if contains_any_phrase(
        &normalized,
        &[
            "waiting for approval",
            "approval required",
            "requires approval",
            "needs approval",
            "awaiting approval",
            "waiting for confirmation",
            "permission required",
            "confirm to continue",
        ],
    ) {
        return Some(AgentStatus::Blocked);
    }
    if contains_any_phrase(
        &normalized,
        &[
            "task complete",
            "task completed",
            "completed successfully",
            "implementation complete",
            "changes applied",
            "all set",
            "finished",
            "done",
        ],
    ) {
        return Some(AgentStatus::Done);
    }
    if contains_any_phrase(
        &normalized,
        &[
            "thinking",
            "working",
            "running",
            "processing",
            "generating",
            "editing",
            "reading",
            "searching",
            "calling tool",
            "executing",
            "applying",
            "analyzing",
            "planning",
        ],
    ) {
        return Some(AgentStatus::Working);
    }
    if contains_any_phrase(
        &normalized,
        &[
            "waiting for input",
            "ready for your next",
            "what would you like",
            "how can i help",
            "ask me anything",
            "type your request",
        ],
    ) {
        return Some(AgentStatus::Idle);
    }

    None
}
