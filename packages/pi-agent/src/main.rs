use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::process::ExitCode;

use ed25519_dalek::VerifyingKey;
use kelp_pi_agent::{
    answer_query, apply_index_schema, approve_operator_token, ask_bind_is_loopback, ask_router,
    default_gold_fixture_dir, evaluate_and_audit_local_policy,
    evaluate_and_audit_local_policy_with_mode, gold_chunk_id_lines, index_db_path,
    init_audit_tracing, install_panic_audit_hook, load_or_generate_identity_key,
    request_operator_approval, rotate_audit_log, run_doctor, run_gold_eval, run_selfcheck,
    run_synthesis_eval, selfcheck_report_payload, sign_envelope, validate_data_dir,
    validate_selfcheck_target, verify_audit_log, verify_audit_log_chain, verify_envelope,
    AskHttpState, PiEnvelopeKind, PiEnvelopeSender, PiLocalPolicyRequest, PiPolicyAction,
    PiPolicyGate, PiPolicyMode, PiWireEnvelope, UnsignedPiWireEnvelope,
    DEFAULT_APPROVAL_TTL_SECONDS, DEFAULT_ASK_BIND, DEFAULT_ASK_MAX_CONCURRENT,
    DEFAULT_ASK_RATE_LIMIT_PER_MINUTE, DEFAULT_DATA_DIR, DEFAULT_GOLD_TOP_K, DEFAULT_KEY_LABEL,
    DEFAULT_NO_ANSWER_THRESHOLD, DEFAULT_QUOTAS,
};
use rusqlite::Connection;
use std::io::{self, BufRead};
use std::time::{SystemTime, UNIX_EPOCH};

fn main() -> ExitCode {
    match run() {
        Ok(()) => ExitCode::SUCCESS,
        Err(code) => code,
    }
}

fn run() -> Result<(), ExitCode> {
    let mut args = std::env::args().skip(1);
    let Some(command) = args.next() else {
        print_usage();
        return Ok(());
    };

    match command.as_str() {
        "approve" => approve_command(args.collect()),
        "approval-request" => approval_request_command(args.collect()),
        "ask" => ask_command(args.collect()),
        "check-data-dir" => check_data_dir(args.collect(), false),
        "doctor" => doctor_command(args.collect()),
        "eval" => eval_command(args.collect()),
        "keygen" => keygen_command(args.collect()),
        "policy-check" => policy_check_command(args.collect()),
        "quota-defaults" => {
            print_quota_defaults();
            Ok(())
        }
        "rotate-audit-log" => rotate_audit_log_command(args.collect()),
        "serve-ask" => serve_ask_command(args.collect()),
        "selfcheck" => selfcheck_command(args.collect()),
        "verify-audit-log" => verify_audit_log_command(args.collect()),
        "wire" => wire_command(args.collect()),
        "start" => check_data_dir(args.collect(), true),
        "version" | "--version" | "-V" => {
            println!("kelp-pi-agent {}", env!("CARGO_PKG_VERSION"));
            Ok(())
        }
        "-h" | "--help" | "help" => {
            print_usage();
            Ok(())
        }
        _ => {
            eprintln!("unknown command: {command}");
            print_usage();
            Err(ExitCode::from(64))
        }
    }
}

fn wire_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut stdio = false;
    let mut trusted_cp_public_key_hex = None;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--stdio" => {
                stdio = true;
                index += 1;
            }
            "--trusted-cp-public-key-hex" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--trusted-cp-public-key-hex requires a value");
                    return Err(ExitCode::from(64));
                };
                trusted_cp_public_key_hex = Some(value.to_string());
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    if !stdio {
        eprintln!("wire requires --stdio");
        return Err(ExitCode::from(64));
    }
    let Some(trusted_cp_public_key_hex) = trusted_cp_public_key_hex else {
        eprintln!("wire requires --trusted-cp-public-key-hex");
        return Err(ExitCode::from(64));
    };
    let trusted_cp_key = verifying_key_from_hex(&trusted_cp_public_key_hex).map_err(|error| {
        eprintln!("invalid trusted control-plane key: {error}");
        ExitCode::from(64)
    })?;
    let identity = load_or_generate_identity_key(&data_dir.join("keys"), DEFAULT_KEY_LABEL)
        .map_err(|error| {
            eprintln!("identity key unavailable: {error}");
            ExitCode::from(78)
        })?;

    for line in io::stdin().lock().lines() {
        let line = line.map_err(|error| {
            eprintln!("wire read failed: {error}");
            ExitCode::from(74)
        })?;
        if line.trim().is_empty() {
            continue;
        }
        let envelope: PiWireEnvelope = serde_json::from_str(&line).map_err(|error| {
            eprintln!("wire envelope JSON invalid: {error}");
            ExitCode::from(65)
        })?;
        verify_envelope(&envelope, &trusted_cp_key).map_err(|error| {
            eprintln!("wire envelope signature invalid: {error}");
            ExitCode::from(77)
        })?;
        if envelope.sender != PiEnvelopeSender::Cp {
            eprintln!("wire envelope sender must be cp");
            return Err(ExitCode::from(77));
        }
        let response = match envelope.kind {
            PiEnvelopeKind::SelfcheckRun => {
                let check_id = envelope
                    .payload
                    .get("check_id")
                    .and_then(serde_json::Value::as_str)
                    .ok_or_else(|| {
                        eprintln!("selfcheck.run payload missing check_id");
                        ExitCode::from(65)
                    })?;
                if let Some(target) = envelope
                    .payload
                    .get("target")
                    .and_then(serde_json::Value::as_str)
                {
                    if let Err(error) = validate_selfcheck_target(&data_dir, target) {
                        return refuse_selfcheck_target(&data_dir, target, &error.to_string());
                    }
                }
                let generated_at = rfc3339_now();
                let report = run_selfcheck(&data_dir);
                sign_envelope(
                    UnsignedPiWireEnvelope {
                        msg_id: format!("{}.report", envelope.msg_id),
                        ts: generated_at.clone(),
                        sender: PiEnvelopeSender::Pi,
                        kind: PiEnvelopeKind::SelfcheckReport,
                        payload: selfcheck_report_payload(check_id, &generated_at, &report),
                    },
                    &identity.signing_key,
                )
                .map_err(|error| {
                    eprintln!("selfcheck.report signing failed: {error}");
                    ExitCode::from(78)
                })?
            }
            other => {
                eprintln!("unsupported wire envelope kind: {other:?}");
                return Err(ExitCode::from(65));
            }
        };
        println!(
            "{}",
            serde_json::to_string(&response).expect("serialize wire response")
        );
    }
    Ok(())
}

fn refuse_selfcheck_target(data_dir: &Path, target: &str, reason: &str) -> Result<(), ExitCode> {
    if let Err(error) = init_audit_tracing(data_dir) {
        eprintln!("audit log init failed while recording selfcheck refusal: {error}");
    } else {
        tracing::warn!(
            event = "selfcheck.target.refused",
            msg = "selfcheck target refused",
            msg_id = "selfcheck-target-refused",
            target = target,
            reason = reason
        );
    }
    eprintln!("selfcheck target refused: {reason}");
    Err(ExitCode::from(77))
}

fn eval_command(args: Vec<String>) -> Result<(), ExitCode> {
    let Some(subcommand) = args.first() else {
        eprintln!("usage: kelp-pi-agent eval <gold|synthesis|chunk-ids> [--fixture-dir PATH] [--top-k N] [--no-answer-threshold FLOAT]");
        return Err(ExitCode::from(64));
    };
    match subcommand.as_str() {
        "chunk-ids" => eval_chunk_ids_command(args[1..].to_vec()),
        "gold" => eval_gold_command(args[1..].to_vec()),
        "synthesis" => eval_synthesis_command(args[1..].to_vec()),
        other => {
            eprintln!("unknown eval command: {other}");
            Err(ExitCode::from(64))
        }
    }
}

fn eval_chunk_ids_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut fixture_dir = default_gold_fixture_dir();
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--fixture-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--fixture-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                fixture_dir = PathBuf::from(value);
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let lines = gold_chunk_id_lines(&fixture_dir).map_err(|error| {
        eprintln!("chunk id eval failed: {error}");
        ExitCode::from(65)
    })?;
    for line in lines {
        println!("{line}");
    }
    Ok(())
}

fn eval_synthesis_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut fixture_dir = default_gold_fixture_dir();
    let mut top_k = DEFAULT_GOLD_TOP_K;
    let mut no_answer_threshold = DEFAULT_NO_ANSWER_THRESHOLD;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--fixture-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--fixture-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                fixture_dir = PathBuf::from(value);
                index += 2;
            }
            "--top-k" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--top-k requires a value");
                    return Err(ExitCode::from(64));
                };
                top_k = parse_positive_usize("--top-k", value)?;
                index += 2;
            }
            "--no-answer-threshold" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--no-answer-threshold requires a value");
                    return Err(ExitCode::from(64));
                };
                no_answer_threshold = parse_non_negative_f64("--no-answer-threshold", value)?;
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let report = run_synthesis_eval(&fixture_dir, top_k, no_answer_threshold).map_err(|error| {
        eprintln!("synthesis eval failed: {error}");
        ExitCode::from(65)
    })?;
    println!(
        "{}",
        serde_json::to_string_pretty(&report).expect("serialize synthesis eval")
    );
    if report.ok() {
        Ok(())
    } else {
        Err(ExitCode::from(65))
    }
}

fn eval_gold_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut fixture_dir = default_gold_fixture_dir();
    let mut top_k = DEFAULT_GOLD_TOP_K;
    let mut no_answer_threshold = DEFAULT_NO_ANSWER_THRESHOLD;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--fixture-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--fixture-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                fixture_dir = PathBuf::from(value);
                index += 2;
            }
            "--top-k" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--top-k requires a value");
                    return Err(ExitCode::from(64));
                };
                top_k = parse_positive_usize("--top-k", value)?;
                index += 2;
            }
            "--no-answer-threshold" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--no-answer-threshold requires a value");
                    return Err(ExitCode::from(64));
                };
                no_answer_threshold = parse_non_negative_f64("--no-answer-threshold", value)?;
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let report = run_gold_eval(&fixture_dir, top_k, no_answer_threshold).map_err(|error| {
        eprintln!("gold eval failed: {error}");
        ExitCode::from(65)
    })?;
    println!(
        "{}",
        serde_json::to_string_pretty(&report).expect("serialize gold eval")
    );
    if report.ok() {
        Ok(())
    } else {
        Err(ExitCode::from(65))
    }
}

fn approval_request_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut gate = None;
    let mut scope_id = "default".to_string();
    let mut ttl_seconds = DEFAULT_APPROVAL_TTL_SECONDS;
    let mut command = None;
    let mut path = None;
    let mut host = None;
    let mut mutating = false;
    let mut allowed = true;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--gate" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--gate requires a value");
                    return Err(ExitCode::from(64));
                };
                let Ok(parsed) = value.parse::<PiPolicyGate>() else {
                    eprintln!("--gate must be scanner-invocation, file-operation, outbound-network-request, or synthesis-request");
                    return Err(ExitCode::from(64));
                };
                gate = Some(parsed);
                index += 2;
            }
            "--scope-id" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--scope-id requires a value");
                    return Err(ExitCode::from(64));
                };
                scope_id = value.to_string();
                index += 2;
            }
            "--ttl-seconds" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--ttl-seconds requires a value");
                    return Err(ExitCode::from(64));
                };
                ttl_seconds = parse_positive_u64("--ttl-seconds", value)?;
                index += 2;
            }
            "--command" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--command requires a value");
                    return Err(ExitCode::from(64));
                };
                command = Some(value.to_string());
                index += 2;
            }
            "--path" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--path requires a value");
                    return Err(ExitCode::from(64));
                };
                path = Some(value.to_string());
                index += 2;
            }
            "--host" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--host requires a value");
                    return Err(ExitCode::from(64));
                };
                host = Some(value.to_string());
                index += 2;
            }
            "--mutating" => {
                mutating = true;
                index += 1;
            }
            "--disallowed" => {
                allowed = false;
                index += 1;
            }
            "--allowed" => {
                allowed = true;
                index += 1;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let Some(gate) = gate else {
        eprintln!("approval-request requires --gate");
        return Err(ExitCode::from(64));
    };
    init_checked_audit(&data_dir)?;

    let request = PiLocalPolicyRequest {
        gate,
        command,
        path,
        host,
        mutating,
        allowed,
    };
    let decision = evaluate_and_audit_local_policy(&request);
    if decision.action != PiPolicyAction::RequireApproval {
        println!(
            "{}",
            serde_json::to_string_pretty(&decision).expect("serialize policy decision")
        );
        return Ok(());
    }
    let approval = request_operator_approval(&data_dir, &decision, &scope_id, ttl_seconds)
        .map_err(|error| {
            eprintln!("approval request failed: {error}");
            ExitCode::from(65)
        })?;
    println!(
        "{}",
        serde_json::to_string_pretty(&approval).expect("serialize approval")
    );
    Ok(())
}

fn approve_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut token = None;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            value if value.starts_with('-') => {
                eprintln!("unknown argument: {value}");
                return Err(ExitCode::from(64));
            }
            value => {
                if token.is_some() {
                    eprintln!("approve accepts exactly one token");
                    return Err(ExitCode::from(64));
                }
                token = Some(value.to_string());
                index += 1;
            }
        }
    }

    let Some(token) = token else {
        eprintln!("approve requires a token");
        return Err(ExitCode::from(64));
    };
    init_checked_audit(&data_dir)?;

    let approval = approve_operator_token(&data_dir, &token).map_err(|error| {
        eprintln!("approve failed: {error}");
        ExitCode::from(65)
    })?;
    println!(
        "{}",
        serde_json::to_string_pretty(&approval).expect("serialize approval")
    );
    Ok(())
}

fn ask_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut db_path = None;
    let mut top_k = 5_usize;
    let mut no_answer_threshold = DEFAULT_NO_ANSWER_THRESHOLD;
    let mut synthesize = false;
    let mut query_parts = Vec::new();
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--db" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--db requires a value");
                    return Err(ExitCode::from(64));
                };
                db_path = Some(PathBuf::from(value));
                index += 2;
            }
            "--top-k" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--top-k requires a value");
                    return Err(ExitCode::from(64));
                };
                let Ok(parsed) = value.parse::<usize>() else {
                    eprintln!("--top-k must be a positive integer");
                    return Err(ExitCode::from(64));
                };
                top_k = parsed.max(1);
                index += 2;
            }
            "--no-answer-threshold" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--no-answer-threshold requires a value");
                    return Err(ExitCode::from(64));
                };
                no_answer_threshold = parse_non_negative_f64("--no-answer-threshold", value)?;
                index += 2;
            }
            "--synthesize" => {
                synthesize = true;
                index += 1;
            }
            other => {
                query_parts.push(other.to_string());
                index += 1;
            }
        }
    }

    if query_parts.is_empty() {
        eprintln!("ask requires a query");
        return Err(ExitCode::from(64));
    }

    let query = query_parts.join(" ");
    let db_path = db_path.unwrap_or_else(|| index_db_path(&data_dir));
    if synthesize {
        init_checked_audit(&data_dir)?;
        let decision = evaluate_and_audit_local_policy(&PiLocalPolicyRequest {
            gate: PiPolicyGate::SynthesisRequest,
            command: Some("ask --synthesize".to_string()),
            path: None,
            host: None,
            mutating: false,
            allowed: true,
        });
        if decision.action != PiPolicyAction::Allow {
            eprintln!("synthesis refused by policy: {}", decision.reason);
            return Err(ExitCode::from(77));
        }
    }
    match Connection::open(&db_path).and_then(|connection| {
        apply_index_schema(&connection)?;
        answer_query(&connection, &query, top_k, no_answer_threshold)
    }) {
        Ok(response) => {
            println!(
                "{}",
                serde_json::to_string(&response).expect("serialize ask response")
            );
            Ok(())
        }
        Err(error) => {
            eprintln!("ask failed: {error}");
            Err(ExitCode::from(65))
        }
    }
}

fn policy_check_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut gate = None;
    let mut mode = PiPolicyMode::Enforce;
    let mut command = None;
    let mut path = None;
    let mut host = None;
    let mut mutating = false;
    let mut allowed = true;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--gate" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--gate requires a value");
                    return Err(ExitCode::from(64));
                };
                let Ok(parsed) = value.parse::<PiPolicyGate>() else {
                    eprintln!("--gate must be scanner-invocation, file-operation, outbound-network-request, or synthesis-request");
                    return Err(ExitCode::from(64));
                };
                gate = Some(parsed);
                index += 2;
            }
            "--mode" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--mode requires a value");
                    return Err(ExitCode::from(64));
                };
                let Ok(parsed) = value.parse::<PiPolicyMode>() else {
                    eprintln!("--mode must be enforce or dry-run");
                    return Err(ExitCode::from(64));
                };
                mode = parsed;
                index += 2;
            }
            "--dry-run" => {
                mode = PiPolicyMode::DryRun;
                index += 1;
            }
            "--command" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--command requires a value");
                    return Err(ExitCode::from(64));
                };
                command = Some(value.to_string());
                index += 2;
            }
            "--path" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--path requires a value");
                    return Err(ExitCode::from(64));
                };
                path = Some(value.to_string());
                index += 2;
            }
            "--host" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--host requires a value");
                    return Err(ExitCode::from(64));
                };
                host = Some(value.to_string());
                index += 2;
            }
            "--mutating" => {
                mutating = true;
                index += 1;
            }
            "--disallowed" => {
                allowed = false;
                index += 1;
            }
            "--allowed" => {
                allowed = true;
                index += 1;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let Some(gate) = gate else {
        eprintln!("policy-check requires --gate");
        return Err(ExitCode::from(64));
    };

    init_checked_audit(&data_dir)?;

    let request = PiLocalPolicyRequest {
        gate,
        command,
        path,
        host,
        mutating,
        allowed,
    };
    let decision = evaluate_and_audit_local_policy_with_mode(&request, mode);
    println!(
        "{}",
        serde_json::to_string_pretty(&decision).expect("serialize policy decision")
    );
    Ok(())
}

fn init_checked_audit(data_dir: &Path) -> Result<(), ExitCode> {
    if let Err(issues) = validate_data_dir(data_dir) {
        for issue in issues {
            eprintln!("{issue}");
        }
        return Err(ExitCode::from(78));
    }
    if let Err(error) = init_audit_tracing(data_dir) {
        eprintln!("audit tracing init failed: {error}");
        return Err(ExitCode::from(78));
    }
    Ok(())
}

fn serve_ask_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut db_path = None;
    let mut bind: SocketAddr = DEFAULT_ASK_BIND.parse().expect("default ask bind parses");
    let mut top_k = 5_usize;
    let mut no_answer_threshold = DEFAULT_NO_ANSWER_THRESHOLD;
    let mut max_concurrent = DEFAULT_ASK_MAX_CONCURRENT;
    let mut rate_limit_per_minute = DEFAULT_ASK_RATE_LIMIT_PER_MINUTE;
    let mut allow_non_loopback = false;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--db" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--db requires a value");
                    return Err(ExitCode::from(64));
                };
                db_path = Some(PathBuf::from(value));
                index += 2;
            }
            "--bind" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--bind requires a value");
                    return Err(ExitCode::from(64));
                };
                let Ok(parsed) = value.parse::<SocketAddr>() else {
                    eprintln!("--bind must be IP:PORT");
                    return Err(ExitCode::from(64));
                };
                bind = parsed;
                index += 2;
            }
            "--top-k" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--top-k requires a value");
                    return Err(ExitCode::from(64));
                };
                let Ok(parsed) = value.parse::<usize>() else {
                    eprintln!("--top-k must be a positive integer");
                    return Err(ExitCode::from(64));
                };
                top_k = parsed.max(1);
                index += 2;
            }
            "--no-answer-threshold" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--no-answer-threshold requires a value");
                    return Err(ExitCode::from(64));
                };
                no_answer_threshold = parse_non_negative_f64("--no-answer-threshold", value)?;
                index += 2;
            }
            "--max-concurrent" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--max-concurrent requires a value");
                    return Err(ExitCode::from(64));
                };
                max_concurrent = parse_positive_usize("--max-concurrent", value)?;
                index += 2;
            }
            "--rate-limit-per-minute" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--rate-limit-per-minute requires a value");
                    return Err(ExitCode::from(64));
                };
                rate_limit_per_minute = parse_positive_usize("--rate-limit-per-minute", value)?;
                index += 2;
            }
            "--allow-non-loopback" => {
                allow_non_loopback = true;
                index += 1;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    if !ask_bind_is_loopback(&bind) && !allow_non_loopback {
        eprintln!("--bind must be loopback unless --allow-non-loopback is set");
        return Err(ExitCode::from(64));
    }

    let db_path = db_path.unwrap_or_else(|| index_db_path(&data_dir));
    let runtime = tokio::runtime::Builder::new_multi_thread()
        .enable_all()
        .build()
        .map_err(|error| {
            eprintln!("tokio runtime init failed: {error}");
            ExitCode::from(70)
        })?;

    runtime.block_on(async move {
        let listener = tokio::net::TcpListener::bind(bind).await.map_err(|error| {
            eprintln!("ask bind failed: {error}");
            ExitCode::from(69)
        })?;
        let local_addr = listener.local_addr().unwrap_or(bind);
        println!("ask server listening on http://{local_addr}");
        let state = AskHttpState::with_limits(
            db_path,
            top_k,
            no_answer_threshold,
            max_concurrent,
            rate_limit_per_minute,
        );
        axum::serve(
            listener,
            ask_router(state).into_make_service_with_connect_info::<SocketAddr>(),
        )
        .await
        .map_err(|error| {
            eprintln!("ask server failed: {error}");
            ExitCode::from(69)
        })
    })
}

fn doctor_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let report = run_doctor(&data_dir);
    println!(
        "{}",
        serde_json::to_string_pretty(&report).expect("serialize doctor report")
    );
    if report.ok {
        Ok(())
    } else {
        Err(ExitCode::from(78))
    }
}

fn selfcheck_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut signed_envelope = false;
    let mut check_id = "selfcheck-cli".to_string();
    let mut target = None;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--signed-envelope" => {
                signed_envelope = true;
                index += 1;
            }
            "--check-id" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--check-id requires a value");
                    return Err(ExitCode::from(64));
                };
                check_id = value.to_string();
                index += 2;
            }
            "--target" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--target requires a value");
                    return Err(ExitCode::from(64));
                };
                target = Some(value.to_string());
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    if let Some(target) = target.as_deref() {
        if let Err(error) = validate_selfcheck_target(&data_dir, target) {
            return refuse_selfcheck_target(&data_dir, target, &error.to_string());
        }
    }

    let report = run_selfcheck(&data_dir);
    if signed_envelope {
        let generated_at = rfc3339_now();
        let identity = load_or_generate_identity_key(&data_dir.join("keys"), DEFAULT_KEY_LABEL)
            .map_err(|error| {
                eprintln!("identity key unavailable: {error}");
                ExitCode::from(78)
            })?;
        let envelope = sign_envelope(
            UnsignedPiWireEnvelope {
                msg_id: format!("{check_id}.report"),
                ts: generated_at.clone(),
                sender: PiEnvelopeSender::Pi,
                kind: PiEnvelopeKind::SelfcheckReport,
                payload: selfcheck_report_payload(&check_id, &generated_at, &report),
            },
            &identity.signing_key,
        )
        .map_err(|error| {
            eprintln!("selfcheck envelope signing failed: {error}");
            ExitCode::from(78)
        })?;
        println!(
            "{}",
            serde_json::to_string_pretty(&envelope).expect("serialize selfcheck envelope")
        );
    } else {
        println!(
            "{}",
            serde_json::to_string_pretty(&report).expect("serialize selfcheck report")
        );
    }
    if report.ok {
        Ok(())
    } else {
        Err(ExitCode::from(78))
    }
}

fn keygen_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut key_dir = None;
    let mut label = DEFAULT_KEY_LABEL.to_string();
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--key-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--key-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                key_dir = Some(PathBuf::from(value));
                index += 2;
            }
            "--label" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--label requires a value");
                    return Err(ExitCode::from(64));
                };
                label = value.to_string();
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let key_dir = key_dir.unwrap_or_else(|| data_dir.join("keys"));
    match load_or_generate_identity_key(&key_dir, &label) {
        Ok(identity) => {
            println!(
                "{}",
                serde_json::to_string(&identity.metadata).expect("serialize key metadata")
            );
            Ok(())
        }
        Err(error) => {
            eprintln!("keygen failed: {error}");
            Err(ExitCode::from(74))
        }
    }
}

fn verify_audit_log_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut log_file = None;
    let mut key_dir = None;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--log-file" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--log-file requires a value");
                    return Err(ExitCode::from(64));
                };
                log_file = Some(PathBuf::from(value));
                index += 2;
            }
            "--key-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--key-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                key_dir = Some(PathBuf::from(value));
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    if let Some(path) = log_file {
        return match verify_audit_log(&path) {
            Ok(result) => {
                println!(
                    "audit log ok: entries={} head_hash={}",
                    result.entries, result.head_hash
                );
                Ok(())
            }
            Err(error) => {
                eprintln!("audit log invalid: {error}");
                Err(ExitCode::from(65))
            }
        };
    }

    let key_dir = key_dir.unwrap_or_else(|| data_dir.join("keys"));
    match verify_audit_log_chain(&data_dir, &key_dir) {
        Ok(result) => {
            println!(
                "audit log ok: segments={} segment_entries={} active_entries={} entries={} head_hash={}",
                result.segments,
                result.segment_entries,
                result.active_entries,
                result.entries,
                result.head_hash
            );
            Ok(())
        }
        Err(error) => {
            eprintln!("audit log invalid: {error}");
            Err(ExitCode::from(65))
        }
    }
}

fn rotate_audit_log_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut key_dir = None;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--key-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--key-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                key_dir = Some(PathBuf::from(value));
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let key_dir = key_dir.unwrap_or_else(|| data_dir.join("keys"));
    match rotate_audit_log(&data_dir, &key_dir) {
        Ok(rotation) => {
            println!(
                "{}",
                serde_json::to_string_pretty(&rotation.manifest)
                    .expect("serialize audit rotation manifest")
            );
            Ok(())
        }
        Err(error) => {
            eprintln!("audit log rotation failed: {error}");
            Err(ExitCode::from(65))
        }
    }
}

fn check_data_dir(args: Vec<String>, start_mode: bool) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut check_only = false;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = PathBuf::from(value);
                index += 2;
            }
            "--check-only" => {
                check_only = true;
                index += 1;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    if let Err(issues) = validate_data_dir(&data_dir) {
        for issue in issues {
            eprintln!("{issue}");
        }
        return Err(ExitCode::from(78));
    }

    let _panic_hook = if start_mode {
        if let Err(error) = init_audit_tracing(&data_dir) {
            eprintln!("audit tracing init failed: {error}");
            return Err(ExitCode::from(78));
        }
        let panic_hook = install_panic_audit_hook();
        tracing::info!(
            event = "agent.preflight.ok",
            msg = "data dir preflight passed",
            msg_id = "agent-preflight-ok",
            data_dir = %data_dir.display()
        );
        Some(panic_hook)
    } else {
        None
    };

    if start_mode && !check_only {
        return run_daemon(&data_dir);
    }

    println!("data dir ok: {}", data_dir.display());
    Ok(())
}

fn run_daemon(data_dir: &Path) -> Result<(), ExitCode> {
    let runtime = tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()
        .map_err(|error| {
            eprintln!("daemon runtime init failed: {error}");
            ExitCode::from(69)
        })?;
    runtime.block_on(wait_for_shutdown(data_dir))
}

#[cfg(unix)]
async fn wait_for_shutdown(data_dir: &Path) -> Result<(), ExitCode> {
    use tokio::signal::unix::{signal, SignalKind};

    let mut sigterm = signal(SignalKind::terminate()).map_err(|error| {
        eprintln!("SIGTERM handler init failed: {error}");
        ExitCode::from(69)
    })?;
    let mut sigint = signal(SignalKind::interrupt()).map_err(|error| {
        eprintln!("SIGINT handler init failed: {error}");
        ExitCode::from(69)
    })?;
    tracing::info!(
        event = "agent.daemon.started",
        msg = "daemon started",
        msg_id = "agent-daemon-started",
        data_dir = %data_dir.display()
    );
    let shutdown_signal = tokio::select! {
        _ = sigterm.recv() => "SIGTERM",
        _ = sigint.recv() => "SIGINT",
    };
    tracing::info!(
        event = "agent.daemon.stopped",
        msg = "daemon stopped",
        msg_id = "agent-daemon-stopped",
        shutdown_signal = shutdown_signal
    );
    Ok(())
}

#[cfg(not(unix))]
async fn wait_for_shutdown(data_dir: &Path) -> Result<(), ExitCode> {
    tracing::info!(
        event = "agent.daemon.started",
        msg = "daemon started",
        msg_id = "agent-daemon-started",
        data_dir = %data_dir.display()
    );
    tokio::signal::ctrl_c().await.map_err(|error| {
        eprintln!("shutdown handler failed: {error}");
        ExitCode::from(69)
    })?;
    tracing::info!(
        event = "agent.daemon.stopped",
        msg = "daemon stopped",
        msg_id = "agent-daemon-stopped",
        shutdown_signal = "ctrl_c"
    );
    Ok(())
}

fn rfc3339_now() -> String {
    let seconds = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|duration| duration.as_secs())
        .unwrap_or(0);
    let day = seconds / 86_400;
    let seconds_of_day = seconds % 86_400;
    let (year, month, day_of_month) = civil_from_unix_day(day as i64);
    let hour = seconds_of_day / 3600;
    let minute = (seconds_of_day % 3600) / 60;
    let second = seconds_of_day % 60;
    format!("{year:04}-{month:02}-{day_of_month:02}T{hour:02}:{minute:02}:{second:02}Z")
}

fn civil_from_unix_day(day: i64) -> (i64, u32, u32) {
    let day = day + 719_468;
    let era = if day >= 0 { day } else { day - 146_096 } / 146_097;
    let day_of_era = day - era * 146_097;
    let year_of_era =
        (day_of_era - day_of_era / 1_460 + day_of_era / 36_524 - day_of_era / 146_096) / 365;
    let year = year_of_era + era * 400;
    let day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    let month_prime = (5 * day_of_year + 2) / 153;
    let day_of_month = day_of_year - (153 * month_prime + 2) / 5 + 1;
    let month = month_prime + if month_prime < 10 { 3 } else { -9 };
    let year = year + if month <= 2 { 1 } else { 0 };
    (year, month as u32, day_of_month as u32)
}

fn verifying_key_from_hex(hex: &str) -> Result<VerifyingKey, String> {
    let bytes = decode_hex(hex)?;
    VerifyingKey::try_from(bytes.as_slice()).map_err(|error| error.to_string())
}

fn decode_hex(text: &str) -> Result<Vec<u8>, String> {
    if !text.len().is_multiple_of(2) {
        return Err("hex length is odd".to_string());
    }
    let mut bytes = Vec::with_capacity(text.len() / 2);
    for pair in text.as_bytes().chunks_exact(2) {
        let high = decode_hex_nibble(pair[0])?;
        let low = decode_hex_nibble(pair[1])?;
        bytes.push((high << 4) | low);
    }
    Ok(bytes)
}

fn decode_hex_nibble(byte: u8) -> Result<u8, String> {
    match byte {
        b'0'..=b'9' => Ok(byte - b'0'),
        b'a'..=b'f' => Ok(byte - b'a' + 10),
        b'A'..=b'F' => Ok(byte - b'A' + 10),
        _ => Err("invalid hex byte".to_string()),
    }
}

fn print_usage() {
    eprintln!(
        "usage: kelp-pi-agent ask QUERY [--data-dir PATH] [--db PATH] [--top-k N] [--no-answer-threshold FLOAT] [--synthesize]"
    );
    eprintln!(
        "usage: kelp-pi-agent approval-request --gate GATE [--scope-id ID] [--ttl-seconds N] [--command CMD] [--path PATH] [--host HOST] [--mutating] [--allowed|--disallowed] [--data-dir PATH]"
    );
    eprintln!("usage: kelp-pi-agent approve TOKEN [--data-dir PATH]");
    eprintln!("usage: kelp-pi-agent check-data-dir [--data-dir PATH]");
    eprintln!("usage: kelp-pi-agent doctor [--data-dir PATH]");
    eprintln!(
        "usage: kelp-pi-agent eval <gold|synthesis|chunk-ids> [--fixture-dir PATH] [--top-k N] [--no-answer-threshold FLOAT]"
    );
    eprintln!("usage: kelp-pi-agent keygen [--data-dir PATH] [--key-dir PATH] [--label LABEL]");
    eprintln!(
        "usage: kelp-pi-agent policy-check --gate GATE [--mode enforce|dry-run] [--dry-run] [--command CMD] [--path PATH] [--host HOST] [--mutating] [--allowed|--disallowed] [--data-dir PATH]"
    );
    eprintln!("usage: kelp-pi-agent quota-defaults");
    eprintln!("usage: kelp-pi-agent rotate-audit-log [--data-dir PATH] [--key-dir PATH]");
    eprintln!(
        "usage: kelp-pi-agent serve-ask [--data-dir PATH] [--db PATH] [--bind IP:PORT] [--top-k N] [--no-answer-threshold FLOAT] [--max-concurrent N] [--rate-limit-per-minute N] [--allow-non-loopback]"
    );
    eprintln!(
        "usage: kelp-pi-agent selfcheck [--data-dir PATH] [--target TARGET] [--signed-envelope] [--check-id ID]"
    );
    eprintln!("usage: kelp-pi-agent start [--data-dir PATH] --check-only");
    eprintln!("usage: kelp-pi-agent version");
    eprintln!(
        "usage: kelp-pi-agent verify-audit-log [--data-dir PATH] [--key-dir PATH] [--log-file PATH]"
    );
    eprintln!(
        "usage: kelp-pi-agent wire --stdio --trusted-cp-public-key-hex HEX [--data-dir PATH]"
    );
}

fn parse_non_negative_f64(flag: &str, value: &str) -> Result<f64, ExitCode> {
    let Ok(parsed) = value.parse::<f64>() else {
        eprintln!("{flag} must be a finite number");
        return Err(ExitCode::from(64));
    };
    if !parsed.is_finite() || parsed < 0.0 {
        eprintln!("{flag} must be a finite non-negative number");
        return Err(ExitCode::from(64));
    }
    Ok(parsed)
}

fn parse_positive_usize(flag: &str, value: &str) -> Result<usize, ExitCode> {
    let Ok(parsed) = value.parse::<usize>() else {
        eprintln!("{flag} must be a positive integer");
        return Err(ExitCode::from(64));
    };
    if parsed == 0 {
        eprintln!("{flag} must be a positive integer");
        return Err(ExitCode::from(64));
    }
    Ok(parsed)
}

fn parse_positive_u64(flag: &str, value: &str) -> Result<u64, ExitCode> {
    let Ok(parsed) = value.parse::<u64>() else {
        eprintln!("{flag} must be a positive integer");
        return Err(ExitCode::from(64));
    };
    if parsed == 0 {
        eprintln!("{flag} must be a positive integer");
        return Err(ExitCode::from(64));
    }
    Ok(parsed)
}

fn print_quota_defaults() {
    println!(
        "{{\"corpus_bytes\":{},\"uploads_bytes\":{},\"index_bytes\":{},\"audit_log_bytes\":{}}}",
        DEFAULT_QUOTAS.corpus_bytes,
        DEFAULT_QUOTAS.uploads_bytes,
        DEFAULT_QUOTAS.index_bytes,
        DEFAULT_QUOTAS.audit_log_bytes
    );
}
