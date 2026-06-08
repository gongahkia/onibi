use super::{ProviderEventUpdate, SessionInfo, SessionLifecycle};
use anyhow::{anyhow, Result};
use bytes::Bytes;
use std::collections::HashMap;

pub(super) fn normalize_session_name(raw: &str) -> Option<String> {
    let trimmed = raw.trim();
    (!trimmed.is_empty()).then(|| trimmed.to_string())
}

pub(super) fn resolve_provider_event_session(
    sessions: &HashMap<String, SessionInfo>,
    update: &ProviderEventUpdate,
) -> Option<String> {
    if let Some(session_id) = update.session_id.as_deref() {
        if sessions.contains_key(session_id) {
            return Some(session_id.to_string());
        }
    }

    for session in sessions.values() {
        let Some(provider) = session.provider.as_ref() else {
            continue;
        };
        if update
            .provider_session_id
            .as_deref()
            .is_some_and(|id| provider.provider_session_id.as_deref() == Some(id))
            || update
                .conversation_id
                .as_deref()
                .is_some_and(|id| provider.conversation_id.as_deref() == Some(id))
        {
            return Some(session.id.clone());
        }
    }

    let agent = update.agent.as_str();
    let cwd = update.cwd.as_deref();
    let matching = sessions
        .values()
        .filter(|session| {
            session.lifecycle == SessionLifecycle::Running
                && session
                    .agent
                    .as_deref()
                    .is_some_and(|candidate| candidate.eq_ignore_ascii_case(agent))
                && cwd.is_none_or(|cwd| session.cwd.as_deref() == Some(cwd))
        })
        .map(|session| session.id.clone())
        .collect::<Vec<_>>();
    if matching.len() == 1 {
        return matching.into_iter().next();
    }

    let matching = sessions
        .values()
        .filter(|session| {
            session.lifecycle == SessionLifecycle::Running
                && session
                    .agent
                    .as_deref()
                    .is_some_and(|candidate| candidate.eq_ignore_ascii_case(agent))
        })
        .map(|session| session.id.clone())
        .collect::<Vec<_>>();
    (matching.len() == 1).then(|| matching[0].clone())
}

pub(super) fn key_to_bytes(key: &str) -> Result<Bytes> {
    let bytes = match key {
        "Enter" | "Return" => b"\r".to_vec(),
        "Tab" => b"\t".to_vec(),
        "Escape" | "Esc" => b"\x1b".to_vec(),
        "Backspace" => b"\x7f".to_vec(),
        "Ctrl+C" | "C-c" => b"\x03".to_vec(),
        "Ctrl+D" | "C-d" => b"\x04".to_vec(),
        "Ctrl+Z" | "C-z" => b"\x1a".to_vec(),
        other if other.starts_with("Text:") => other["Text:".len()..].as_bytes().to_vec(),
        other if other.len() == 1 => other.as_bytes().to_vec(),
        other => return Err(anyhow!("unsupported key: {other}")),
    };
    Ok(Bytes::from(bytes))
}
