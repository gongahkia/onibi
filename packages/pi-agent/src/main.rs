use std::fs;
use std::net::SocketAddr;
use std::path::{Path, PathBuf};
use std::process::ExitCode;

use ed25519_dalek::VerifyingKey;
use kelp_pi_agent::{
    answer_query, apply_index_schema, apply_policy_push, apply_scope_set, approve_operator_token,
    ask_bind_is_loopback, ask_router, assemble_pi_audit_bundle, decision_after_approval,
    default_gold_fixture_dir, default_scanner_stability_fixture_dir, enforce_nuclei_templates_pin,
    enforce_storage_quota, ensure_targets_in_scope, evaluate_and_audit_local_policy,
    evaluate_and_audit_local_policy_with_mode, evaluate_scan_thermal_guard, evaluate_zap_guard,
    gold_chunk_id_lines, index_db_path, init_audit_tracing, install_panic_audit_hook,
    load_active_scope, load_or_generate_identity_key, load_pi_bundle_transfer,
    load_storage_quota_config, probe_pinned_nmap_version, request_operator_approval,
    rotate_audit_log, run_doctor, run_gold_eval, run_scanner_stability_eval,
    run_scanner_with_limits, run_selfcheck, run_synthesis_eval, scanner_enforced_args,
    selfcheck_report_payload, sign_envelope, unix_millis_now, validate_data_dir,
    validate_selfcheck_target, verify_and_stage_firmware_update, verify_audit_log,
    verify_audit_log_chain, verify_envelope, wipe_data_dir, write_network_hardening_files,
    write_nuclei_findings_document, AskHttpState, IdentityKey, NmapVersion, NucleiTemplatesPin,
    OutboundEndpoint, PiEnvelopeKind, PiEnvelopeSender, PiLocalPolicyRequest,
    PiNetworkHardeningConfig, PiPolicyAction, PiPolicyGate, PiPolicyMode, PiWireEnvelope,
    PolicyTrustState, ScannerLimits, ScopeError, ScopeTarget, ScopeTargetType, StorageQuotaError,
    StorageQuotaScope, StoredPolicyPack, ThermalScanDecision, TrustedControlPlaneKey,
    UnsignedPiWireEnvelope, ZapDecision, CURRENT_POLICY_FILE, DEFAULT_AGENT_CONFIG_PATH,
    DEFAULT_APPROVAL_TTL_SECONDS, DEFAULT_ASK_BIND, DEFAULT_ASK_MAX_CONCURRENT,
    DEFAULT_ASK_RATE_LIMIT_PER_MINUTE, DEFAULT_DATA_DIR, DEFAULT_GOLD_TOP_K, DEFAULT_KEY_LABEL,
    DEFAULT_NO_ANSWER_THRESHOLD, DEFAULT_QUOTAS,
};
use rusqlite::Connection;
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use sha2::{Digest, Sha256};
use std::io::{self, BufRead};
use std::time::{Duration, SystemTime, UNIX_EPOCH};

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
        "bundle" => bundle_command(args.collect()),
        "check-data-dir" => check_data_dir(args.collect(), false),
        "doctor" => doctor_command(args.collect()),
        "eval" => eval_command(args.collect()),
        "firmware-update" => firmware_update_command(args.collect()),
        "hardening" => hardening_command(args.collect()),
        "keygen" => keygen_command(args.collect()),
        "normalize" => normalize_command(args.collect()),
        "outbox" => outbox_command(args.collect()),
        "policy" => policy_command(args.collect()),
        "policy-check" => policy_check_command(args.collect()),
        "quota-defaults" => {
            print_quota_defaults();
            Ok(())
        }
        "rotate-audit-log" => rotate_audit_log_command(args.collect()),
        "scan" => scan_command(args.collect()),
        "serve-ask" => serve_ask_command(args.collect()),
        "selfcheck" => selfcheck_command(args.collect()),
        "upload" => upload_command(args.collect()),
        "verify-audit-log" => verify_audit_log_command(args.collect()),
        "wipe" => wipe_command(args.collect()),
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
    let trusted_cp_key_id =
        trusted_key_id_from_raw_hex(&trusted_cp_public_key_hex).map_err(|error| {
            eprintln!("invalid trusted control-plane key: {error}");
            ExitCode::from(64)
        })?;
    let trusted_cp_delivery_key = TrustedControlPlaneKey {
        key_id: trusted_cp_key_id,
        public_key_raw_hex: trusted_cp_public_key_hex.clone(),
        state: PolicyTrustState::Trusted,
    };
    let identity = load_or_generate_identity_key(&data_dir.join("keys"), DEFAULT_KEY_LABEL)
        .map_err(|error| {
            eprintln!("identity key unavailable: {error}");
            ExitCode::from(78)
        })?;
    let mut audit_started = false;

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
        let responses = match envelope.kind {
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
                vec![sign_envelope(
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
                })?]
            }
            PiEnvelopeKind::ScopeSet => {
                apply_scope_set(&data_dir, &envelope, &trusted_cp_key).map_err(|error| {
                    eprintln!("scope.set refused: {error}");
                    ExitCode::from(77)
                })?;
                vec![sign_envelope(
                    UnsignedPiWireEnvelope {
                        msg_id: format!("{}.accepted", envelope.msg_id),
                        ts: rfc3339_now(),
                        sender: PiEnvelopeSender::Pi,
                        kind: PiEnvelopeKind::ScopeSet,
                        payload: envelope.payload.clone(),
                    },
                    &identity.signing_key,
                )
                .map_err(|error| {
                    eprintln!("scope.set receipt signing failed: {error}");
                    ExitCode::from(78)
                })?]
            }
            PiEnvelopeKind::PolicyPush => {
                if !audit_started {
                    init_checked_audit(&data_dir)?;
                    audit_started = true;
                }
                policy_push_responses(
                    &data_dir,
                    &envelope,
                    &identity,
                    std::slice::from_ref(&trusted_cp_delivery_key),
                )
                .map_err(|error| {
                    eprintln!("policy.push refused: {error}");
                    ExitCode::from(77)
                })?
            }
            PiEnvelopeKind::ScanRequest => {
                if !audit_started {
                    init_checked_audit(&data_dir)?;
                    audit_started = true;
                }
                scan_request_responses(&data_dir, &envelope, &identity).map_err(|error| {
                    eprintln!("scan.request failed: {error}");
                    ExitCode::from(65)
                })?
            }
            PiEnvelopeKind::BundleFetch => bundle_fetch_responses(&data_dir, &envelope, &identity)
                .map_err(|error| {
                    eprintln!("bundle.fetch failed: {error}");
                    ExitCode::from(65)
                })?,
            other => {
                eprintln!("unsupported wire envelope kind: {other:?}");
                return Err(ExitCode::from(65));
            }
        };
        for response in responses {
            println!(
                "{}",
                serde_json::to_string(&response).expect("serialize wire response")
            );
        }
    }
    Ok(())
}

fn policy_push_responses(
    data_dir: &Path,
    envelope: &PiWireEnvelope,
    identity: &IdentityKey,
    trusted_keys: &[TrustedControlPlaneKey],
) -> Result<Vec<PiWireEnvelope>, String> {
    let receipt = apply_and_audit_policy_push(data_dir, envelope, trusted_keys)?;
    let payload = serde_json::to_value(&receipt)
        .map_err(|error| error.to_string())?
        .as_object()
        .cloned()
        .ok_or_else(|| "policy push receipt must be an object".to_string())?;
    Ok(vec![sign_envelope(
        UnsignedPiWireEnvelope {
            msg_id: format!("{}.accepted", envelope.msg_id),
            ts: rfc3339_now(),
            sender: PiEnvelopeSender::Pi,
            kind: PiEnvelopeKind::PolicyPush,
            payload,
        },
        &identity.signing_key,
    )
    .map_err(|error| error.to_string())?])
}

fn apply_and_audit_policy_push(
    data_dir: &Path,
    envelope: &PiWireEnvelope,
    trusted_keys: &[TrustedControlPlaneKey],
) -> Result<kelp_pi_agent::PolicyPushReceipt, String> {
    let receipt =
        apply_policy_push(data_dir, envelope, trusted_keys).map_err(|error| error.to_string())?;
    tracing::info!(
        event = "policy.push.accepted",
        msg = "policy push accepted",
        msg_id = "policy-push-accepted",
        policy_pack_id = receipt.policy_pack_id.as_str(),
        signer_key_id = receipt.signer_key_id.as_str(),
        trust_epoch = receipt.trust_epoch,
        path = receipt.path.as_str()
    );
    Ok(receipt)
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

#[derive(Debug, Deserialize)]
struct WireScanRequestPayload {
    run_id: String,
    scope_id: String,
    scanner: String,
    targets: Vec<ScopeTarget>,
    #[serde(default)]
    options: Map<String, Value>,
}

#[derive(Debug, Deserialize)]
struct WireBundleFetchPayload {
    request_id: String,
    bundle_id: String,
    run_id: Option<String>,
}

fn bundle_fetch_responses(
    data_dir: &Path,
    envelope: &PiWireEnvelope,
    identity: &IdentityKey,
) -> Result<Vec<PiWireEnvelope>, String> {
    let payload: WireBundleFetchPayload =
        serde_json::from_value(Value::Object(envelope.payload.clone()))
            .map_err(|error| error.to_string())?;
    let transfer = load_pi_bundle_transfer(data_dir, &payload.bundle_id, payload.run_id.as_deref())
        .map_err(|error| error.to_string())?;
    let mut response_payload = serde_json::to_value(&transfer)
        .map_err(|error| error.to_string())?
        .as_object()
        .cloned()
        .ok_or_else(|| "bundle transfer payload must be an object".to_string())?;
    response_payload.insert(
        "request_id".to_string(),
        Value::String(payload.request_id.clone()),
    );
    tracing::info!(
        event = "bundle.fetch",
        msg = "bundle fetched",
        msg_id = "bundle-fetch",
        request_id = payload.request_id.as_str(),
        bundle_id = transfer.bundle_id.as_str(),
        run_id = transfer.run_id.as_str(),
        size_bytes = transfer.size_bytes
    );
    Ok(vec![sign_envelope(
        UnsignedPiWireEnvelope {
            msg_id: format!("{}.bundle", envelope.msg_id),
            ts: rfc3339_now(),
            sender: PiEnvelopeSender::Pi,
            kind: PiEnvelopeKind::BundleFetch,
            payload: response_payload,
        },
        &identity.signing_key,
    )
    .map_err(|error| error.to_string())?])
}

fn scan_request_responses(
    data_dir: &Path,
    envelope: &PiWireEnvelope,
    identity: &IdentityKey,
) -> Result<Vec<PiWireEnvelope>, String> {
    let payload: WireScanRequestPayload =
        serde_json::from_value(Value::Object(envelope.payload.clone()))
            .map_err(|error| error.to_string())?;
    if !matches!(payload.scanner.as_str(), "nuclei" | "nmap" | "zap") {
        return Err("scan.request scanner must be nuclei, nmap, or zap".to_string());
    }
    let started_at = rfc3339_now();
    let mut seq = 0_u64;
    let mut responses = Vec::new();
    let target_values = scan_request_target_values(&payload.targets);
    let limits = match scan_limits_from_options(&payload.options) {
        Ok(limits) => limits,
        Err(error) => {
            responses.push(signed_scan_complete_from_draft(
                envelope,
                identity,
                ScanCompleteDraft {
                    run_id: &payload.run_id,
                    status: "refused",
                    started_at: &started_at,
                    finished_at: &rfc3339_now(),
                    error: Some(error),
                    metadata: Map::new(),
                },
            )?);
            return Ok(responses);
        }
    };
    if let Err(error) = validate_scanner_limits(&payload.scanner, target_values.len(), limits) {
        responses.push(signed_scan_complete_from_draft(
            envelope,
            identity,
            ScanCompleteDraft {
                run_id: &payload.run_id,
                status: "refused",
                started_at: &started_at,
                finished_at: &rfc3339_now(),
                error: Some(error),
                metadata: Map::new(),
            },
        )?);
        return Ok(responses);
    }
    let min_free_bytes = match scan_request_min_free_bytes(&payload.options) {
        Ok(value) => value,
        Err(error) => {
            responses.push(signed_scan_complete_from_draft(
                envelope,
                identity,
                ScanCompleteDraft {
                    run_id: &payload.run_id,
                    status: "refused",
                    started_at: &started_at,
                    finished_at: &rfc3339_now(),
                    error: Some(error),
                    metadata: Map::new(),
                },
            )?);
            return Ok(responses);
        }
    };
    if let Err(error) =
        enforce_command_storage_quota(data_dir, StorageQuotaScope::Scan, None, min_free_bytes)
    {
        audit_storage_quota_refusal(StorageQuotaScope::Scan, &error);
        responses.push(signed_scan_complete_from_draft(
            envelope,
            identity,
            ScanCompleteDraft {
                run_id: &payload.run_id,
                status: "refused",
                started_at: &started_at,
                finished_at: &rfc3339_now(),
                error: Some(error.to_string()),
                metadata: Map::new(),
            },
        )?);
        return Ok(responses);
    }
    let nuclei_templates = if payload.scanner == "nuclei" {
        match enforce_nuclei_templates_pin(
            scan_option_string(&payload.options, "templates_sha").as_deref(),
        ) {
            Ok(pin) => Some(pin),
            Err(error) => {
                responses.push(signed_scan_complete(
                    envelope,
                    identity,
                    &payload.run_id,
                    "refused",
                    &started_at,
                    &rfc3339_now(),
                    Some(error.to_string()),
                )?);
                return Ok(responses);
            }
        }
    } else {
        None
    };
    responses.push(signed_scan_event(
        envelope,
        identity,
        ScanEventDraft {
            run_id: &payload.run_id,
            seq: next_seq(&mut seq),
            phase: "queued",
            ts: &started_at,
            message: "scan request queued",
            data: scan_event_data(
                &payload.scanner,
                &target_values,
                None,
                nuclei_templates.as_ref(),
            ),
        },
    )?);

    let scope = match load_active_scope(data_dir) {
        Ok(scope) => scope,
        Err(error) => {
            responses.push(signed_scan_complete_from_draft(
                envelope,
                identity,
                ScanCompleteDraft {
                    run_id: &payload.run_id,
                    status: "refused",
                    started_at: &started_at,
                    finished_at: &rfc3339_now(),
                    error: Some(error.to_string()),
                    metadata: scanner_metadata(None, nuclei_templates.as_ref()),
                },
            )?);
            return Ok(responses);
        }
    };
    if scope.payload.scope_id != payload.scope_id {
        responses.push(signed_scan_complete_from_draft(
            envelope,
            identity,
            ScanCompleteDraft {
                run_id: &payload.run_id,
                status: "refused",
                started_at: &started_at,
                finished_at: &rfc3339_now(),
                error: Some(format!(
                    "active scope {} does not match request scope {}",
                    scope.payload.scope_id, payload.scope_id
                )),
                metadata: scanner_metadata(None, nuclei_templates.as_ref()),
            },
        )?);
        return Ok(responses);
    }
    if let Err(error) = ensure_targets_in_scope(&scope, &target_values, unix_millis_now()) {
        tracing::warn!(
            event = "scope.violation",
            msg = "scan target refused by scope",
            msg_id = "scope-violation",
            scanner = payload.scanner.as_str(),
            targets = target_values.join(","),
            reason = scan_scope_error_reason(&error).as_str()
        );
        responses.push(signed_scan_complete_from_draft(
            envelope,
            identity,
            ScanCompleteDraft {
                run_id: &payload.run_id,
                status: "refused",
                started_at: &started_at,
                finished_at: &rfc3339_now(),
                error: Some(scan_scope_error_reason(&error)),
                metadata: scanner_metadata(None, nuclei_templates.as_ref()),
            },
        )?);
        return Ok(responses);
    }
    let thermal_guard = evaluate_scan_thermal_guard();
    if thermal_guard.decision == ThermalScanDecision::Refuse {
        responses.push(signed_scan_complete_from_draft(
            envelope,
            identity,
            ScanCompleteDraft {
                run_id: &payload.run_id,
                status: "refused",
                started_at: &started_at,
                finished_at: &rfc3339_now(),
                error: Some(thermal_guard.reason),
                metadata: scanner_metadata(None, nuclei_templates.as_ref()),
            },
        )?);
        return Ok(responses);
    }
    if payload.scanner == "zap" {
        let zap_guard = evaluate_zap_guard(scan_option_bool(&payload.options, "enable_zap"));
        if zap_guard.decision == ZapDecision::Refuse {
            responses.push(signed_scan_complete_from_draft(
                envelope,
                identity,
                ScanCompleteDraft {
                    run_id: &payload.run_id,
                    status: "refused",
                    started_at: &started_at,
                    finished_at: &rfc3339_now(),
                    error: Some(zap_guard.reason),
                    metadata: scanner_metadata(None, nuclei_templates.as_ref()),
                },
            )?);
            return Ok(responses);
        }
    }
    let scanner_bin = scan_option_string(&payload.options, "scanner_bin")
        .unwrap_or_else(|| payload.scanner.clone());
    let nmap_version = if payload.scanner == "nmap" {
        match probe_pinned_nmap_version(&scanner_bin) {
            Ok(version) => Some(version),
            Err(error) => {
                responses.push(signed_scan_complete_from_draft(
                    envelope,
                    identity,
                    ScanCompleteDraft {
                        run_id: &payload.run_id,
                        status: "refused",
                        started_at: &started_at,
                        finished_at: &rfc3339_now(),
                        error: Some(error.to_string()),
                        metadata: scanner_metadata(None, nuclei_templates.as_ref()),
                    },
                )?);
                return Ok(responses);
            }
        }
    } else {
        None
    };

    responses.push(signed_scan_event(
        envelope,
        identity,
        ScanEventDraft {
            run_id: &payload.run_id,
            seq: next_seq(&mut seq),
            phase: "started",
            ts: &started_at,
            message: "scan request started",
            data: scan_event_data(
                &payload.scanner,
                &target_values,
                nmap_version.as_ref(),
                nuclei_templates.as_ref(),
            ),
        },
    )?);
    let decision = evaluate_and_audit_local_policy(&PiLocalPolicyRequest {
        gate: PiPolicyGate::ScannerInvocation,
        command: Some(format!(
            "scan {} {}",
            payload.scanner,
            target_values.join(" ")
        )),
        path: None,
        host: Some(target_values.join(",")),
        mutating: false,
        allowed: true,
    });
    let decision = if let Some(token) = scan_option_string(&payload.options, "approval_token") {
        decision_after_approval(data_dir, &decision, &scope.payload.scope_id, &token)
            .map_err(|error| error.to_string())?
    } else {
        decision
    };
    responses.push(signed_scan_event(
        envelope,
        identity,
        ScanEventDraft {
            run_id: &payload.run_id,
            seq: next_seq(&mut seq),
            phase: "policy-decision",
            ts: &rfc3339_now(),
            message: decision.reason.as_str(),
            data: policy_event_data(&decision, nmap_version.as_ref(), nuclei_templates.as_ref()),
        },
    )?);
    if decision.action != PiPolicyAction::Allow {
        tracing::warn!(
            event = "scan.policy.refused",
            msg = "scan refused by policy",
            msg_id = "scan-policy-refused",
            scanner = payload.scanner.as_str(),
            scope_id = scope.payload.scope_id.as_str(),
            action = decision.action.as_str(),
            reason = decision.reason.as_str()
        );
        responses.push(signed_scan_complete_from_draft(
            envelope,
            identity,
            ScanCompleteDraft {
                run_id: &payload.run_id,
                status: "refused",
                started_at: &started_at,
                finished_at: &rfc3339_now(),
                error: Some(decision.reason),
                metadata: scanner_metadata(nmap_version.as_ref(), nuclei_templates.as_ref()),
            },
        )?);
        return Ok(responses);
    }
    if scan_option_bool(&payload.options, "dry_run") {
        responses.push(signed_scan_complete_from_draft(
            envelope,
            identity,
            ScanCompleteDraft {
                run_id: &payload.run_id,
                status: "succeeded",
                started_at: &started_at,
                finished_at: &rfc3339_now(),
                error: None,
                metadata: scanner_metadata(nmap_version.as_ref(), nuclei_templates.as_ref()),
            },
        )?);
        return Ok(responses);
    }

    if !safe_scan_run_id(&payload.run_id) {
        return Err(
            "scan.request run_id must use only ASCII letters, numbers, dot, dash, or underscore"
                .to_string(),
        );
    }
    let mut run_state = ScanRunState::running(
        payload.run_id.clone(),
        payload.scanner.clone(),
        scope.payload.scope_id.clone(),
        target_values.clone(),
        started_at.clone(),
    );
    write_scan_run_state(data_dir, &run_state).map_err(|error| error.to_string())?;
    let scanner_args = scan_option_strings(&payload.options, "args");
    let outcome = match run_scanner_with_limits(
        &payload.scanner,
        &scanner_bin,
        &scanner_args,
        &target_values,
        limits,
    ) {
        Ok(outcome) => outcome,
        Err(error) => {
            run_state.mark_failed(Some(error.to_string()));
            write_scan_run_state(data_dir, &run_state).map_err(|error| error.to_string())?;
            responses.push(signed_scan_complete_from_draft(
                envelope,
                identity,
                ScanCompleteDraft {
                    run_id: &payload.run_id,
                    status: "refused",
                    started_at: &started_at,
                    finished_at: &rfc3339_now(),
                    error: Some(error.to_string()),
                    metadata: scanner_metadata(nmap_version.as_ref(), nuclei_templates.as_ref()),
                },
            )?);
            return Ok(responses);
        }
    };
    let status = outcome.status;
    if status.success() {
        run_state.mark_succeeded();
    } else if outcome.timed_out {
        run_state.mark_failed(Some(format!(
            "scanner timed out after {} seconds",
            limits.max_scan_duration_seconds.unwrap_or_default()
        )));
    } else {
        run_state.mark_failed(Some(format!("scanner exited with {status}")));
    }
    write_scan_run_state(data_dir, &run_state).map_err(|error| error.to_string())?;
    responses.push(signed_scan_complete_from_draft(
        envelope,
        identity,
        ScanCompleteDraft {
            run_id: &payload.run_id,
            status: if status.success() {
                "succeeded"
            } else {
                "failed"
            },
            started_at: &started_at,
            finished_at: &rfc3339_now(),
            error: if status.success() {
                None
            } else if outcome.timed_out {
                Some(format!(
                    "scanner timed out after {} seconds",
                    limits.max_scan_duration_seconds.unwrap_or_default()
                ))
            } else {
                Some(format!("scanner exited with {status}"))
            },
            metadata: scanner_metadata(nmap_version.as_ref(), nuclei_templates.as_ref()),
        },
    )?);
    Ok(responses)
}

fn next_seq(seq: &mut u64) -> u64 {
    let current = *seq;
    *seq = seq.saturating_add(1);
    current
}

struct ScanEventDraft<'a> {
    run_id: &'a str,
    seq: u64,
    phase: &'a str,
    ts: &'a str,
    message: &'a str,
    data: Map<String, Value>,
}

fn signed_scan_event(
    request: &PiWireEnvelope,
    identity: &IdentityKey,
    event: ScanEventDraft<'_>,
) -> Result<PiWireEnvelope, String> {
    let mut payload = Map::new();
    payload.insert(
        "run_id".to_string(),
        Value::String(event.run_id.to_string()),
    );
    payload.insert("seq".to_string(), Value::from(event.seq));
    payload.insert("phase".to_string(), Value::String(event.phase.to_string()));
    payload.insert("ts".to_string(), Value::String(event.ts.to_string()));
    payload.insert(
        "message".to_string(),
        Value::String(event.message.to_string()),
    );
    payload.insert("data".to_string(), Value::Object(event.data));
    sign_envelope(
        UnsignedPiWireEnvelope {
            msg_id: format!("{}.event.{}", request.msg_id, event.seq),
            ts: event.ts.to_string(),
            sender: PiEnvelopeSender::Pi,
            kind: PiEnvelopeKind::ScanEvent,
            payload,
        },
        &identity.signing_key,
    )
    .map_err(|error| error.to_string())
}

struct ScanCompleteDraft<'a> {
    run_id: &'a str,
    status: &'a str,
    started_at: &'a str,
    finished_at: &'a str,
    error: Option<String>,
    metadata: Map<String, Value>,
}

fn signed_scan_complete(
    request: &PiWireEnvelope,
    identity: &IdentityKey,
    run_id: &str,
    status: &str,
    started_at: &str,
    finished_at: &str,
    error: Option<String>,
) -> Result<PiWireEnvelope, String> {
    signed_scan_complete_from_draft(
        request,
        identity,
        ScanCompleteDraft {
            run_id,
            status,
            started_at,
            finished_at,
            error,
            metadata: Map::new(),
        },
    )
}

fn signed_scan_complete_from_draft(
    request: &PiWireEnvelope,
    identity: &IdentityKey,
    complete: ScanCompleteDraft<'_>,
) -> Result<PiWireEnvelope, String> {
    let mut payload = Map::new();
    payload.insert(
        "run_id".to_string(),
        Value::String(complete.run_id.to_string()),
    );
    payload.insert(
        "status".to_string(),
        Value::String(complete.status.to_string()),
    );
    payload.insert(
        "started_at".to_string(),
        Value::String(complete.started_at.to_string()),
    );
    payload.insert(
        "finished_at".to_string(),
        Value::String(complete.finished_at.to_string()),
    );
    payload.insert("evidence_ids".to_string(), Value::Array(Vec::new()));
    if !complete.metadata.is_empty() {
        payload.insert("metadata".to_string(), Value::Object(complete.metadata));
    }
    if let Some(error) = complete.error {
        payload.insert("error".to_string(), Value::String(error));
    }
    sign_envelope(
        UnsignedPiWireEnvelope {
            msg_id: format!("{}.complete", request.msg_id),
            ts: complete.finished_at.to_string(),
            sender: PiEnvelopeSender::Pi,
            kind: PiEnvelopeKind::ScanComplete,
            payload,
        },
        &identity.signing_key,
    )
    .map_err(|error| error.to_string())
}

fn scan_request_target_values(targets: &[ScopeTarget]) -> Vec<String> {
    let mut values = Vec::new();
    for target in targets {
        match target.ports.as_deref() {
            Some(ports)
                if matches!(
                    target.target_type,
                    ScopeTargetType::Host | ScopeTargetType::Ip
                ) =>
            {
                values.extend(ports.iter().map(|port| format!("{}:{port}", target.value)));
            }
            _ => values.push(target.value.clone()),
        }
    }
    values
}

fn scan_event_data(
    scanner: &str,
    targets: &[String],
    nmap_version: Option<&NmapVersion>,
    nuclei_templates: Option<&NucleiTemplatesPin>,
) -> Map<String, Value> {
    let mut data = Map::new();
    data.insert("scanner".to_string(), Value::String(scanner.to_string()));
    data.insert(
        "targets".to_string(),
        Value::Array(targets.iter().cloned().map(Value::String).collect()),
    );
    add_scanner_metadata(&mut data, nmap_version, nuclei_templates);
    data
}

fn add_scanner_metadata(
    data: &mut Map<String, Value>,
    nmap_version: Option<&NmapVersion>,
    nuclei_templates: Option<&NucleiTemplatesPin>,
) {
    if let Some(nmap_version) = nmap_version {
        data.insert(
            "nmap_runtime_version".to_string(),
            Value::String(nmap_version.runtime_version.clone()),
        );
        data.insert(
            "nmap_debian_package_version".to_string(),
            Value::String(nmap_version.debian_package_version.to_string()),
        );
    }
    if let Some(nuclei_templates) = nuclei_templates {
        data.insert(
            "nuclei_templates_revision".to_string(),
            Value::String(nuclei_templates.revision.to_string()),
        );
    }
}

fn scanner_metadata(
    nmap_version: Option<&NmapVersion>,
    nuclei_templates: Option<&NucleiTemplatesPin>,
) -> Map<String, Value> {
    let mut data = Map::new();
    add_scanner_metadata(&mut data, nmap_version, nuclei_templates);
    data
}

fn policy_event_data(
    decision: &kelp_pi_agent::PiLocalPolicyDecision,
    nmap_version: Option<&NmapVersion>,
    nuclei_templates: Option<&NucleiTemplatesPin>,
) -> Map<String, Value> {
    let mut data = Map::new();
    data.insert(
        "action".to_string(),
        Value::String(decision.action.as_str().to_string()),
    );
    data.insert(
        "matched_rule_ids".to_string(),
        Value::Array(
            decision
                .matched_rule_ids
                .iter()
                .cloned()
                .map(Value::String)
                .collect(),
        ),
    );
    add_scanner_metadata(&mut data, nmap_version, nuclei_templates);
    data
}

fn scan_option_bool(options: &Map<String, Value>, key: &str) -> bool {
    options.get(key).and_then(Value::as_bool).unwrap_or(false)
}

fn scan_option_string(options: &Map<String, Value>, key: &str) -> Option<String> {
    options.get(key)?.as_str().map(str::to_string)
}

fn scan_option_strings(options: &Map<String, Value>, key: &str) -> Vec<String> {
    options
        .get(key)
        .and_then(Value::as_array)
        .map(|values| {
            values
                .iter()
                .filter_map(Value::as_str)
                .map(str::to_string)
                .collect()
        })
        .unwrap_or_default()
}

fn scan_option_u32(options: &Map<String, Value>, key: &str) -> Result<Option<u32>, String> {
    let Some(value) = options.get(key) else {
        return Ok(None);
    };
    let Some(raw) = value.as_u64() else {
        return Err(format!("{key} must be a positive integer"));
    };
    if raw == 0 || raw > u32::MAX as u64 {
        return Err(format!("{key} must be a positive 32-bit integer"));
    }
    Ok(Some(raw as u32))
}

fn scan_option_usize(options: &Map<String, Value>, key: &str) -> Result<Option<usize>, String> {
    let Some(value) = options.get(key) else {
        return Ok(None);
    };
    let Some(raw) = value.as_u64() else {
        return Err(format!("{key} must be a positive integer"));
    };
    if raw == 0 || raw > usize::MAX as u64 {
        return Err(format!("{key} must be a positive integer"));
    }
    Ok(Some(raw as usize))
}

fn scan_option_u64(options: &Map<String, Value>, key: &str) -> Result<Option<u64>, String> {
    let Some(value) = options.get(key) else {
        return Ok(None);
    };
    let Some(raw) = value.as_u64() else {
        return Err(format!("{key} must be a positive integer"));
    };
    if raw == 0 {
        return Err(format!("{key} must be a positive integer"));
    }
    Ok(Some(raw))
}

fn scan_request_min_free_bytes(options: &Map<String, Value>) -> Result<Option<u64>, String> {
    scan_option_u64(options, "min_free_bytes")
}

fn scan_limits_from_options(options: &Map<String, Value>) -> Result<ScannerLimits, String> {
    ScannerLimits {
        max_requests_per_second: scan_option_u32(options, "max_requests_per_second")?,
        max_concurrent_targets: scan_option_usize(options, "max_concurrent_targets")?,
        max_scan_duration_seconds: scan_option_u64(options, "max_scan_duration_seconds")?,
    }
    .validate()
    .map_err(|error| error.to_string())
}

fn validate_scanner_limits(
    scanner: &str,
    target_count: usize,
    limits: ScannerLimits,
) -> Result<Vec<String>, String> {
    limits
        .enforce_target_count(target_count)
        .map_err(|error| error.to_string())?;
    scanner_enforced_args(scanner, &limits).map_err(|error| error.to_string())
}

fn eval_command(args: Vec<String>) -> Result<(), ExitCode> {
    let Some(subcommand) = args.first() else {
        eprintln!("usage: kelp-pi-agent eval <gold|synthesis|chunk-ids|scanner-stability> [--fixture-dir PATH] [--top-k N] [--no-answer-threshold FLOAT]");
        return Err(ExitCode::from(64));
    };
    match subcommand.as_str() {
        "chunk-ids" => eval_chunk_ids_command(args[1..].to_vec()),
        "gold" => eval_gold_command(args[1..].to_vec()),
        "scanner-stability" => eval_scanner_stability_command(args[1..].to_vec()),
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

fn eval_scanner_stability_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut fixture_dir = default_scanner_stability_fixture_dir();
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

    let report = run_scanner_stability_eval(&fixture_dir).map_err(|error| {
        eprintln!("scanner stability eval failed: {error}");
        ExitCode::from(65)
    })?;
    println!(
        "{}",
        serde_json::to_string_pretty(&report).expect("serialize scanner stability eval")
    );
    if report.ok() {
        Ok(())
    } else {
        Err(ExitCode::from(65))
    }
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

fn hardening_command(args: Vec<String>) -> Result<(), ExitCode> {
    let Some(subcommand) = args.first() else {
        eprintln!("usage: kelp-pi-agent hardening render-network ...");
        return Err(ExitCode::from(64));
    };
    match subcommand.as_str() {
        "render-network" => hardening_render_network_command(args[1..].to_vec()),
        other => {
            eprintln!("unknown hardening subcommand: {other}");
            Err(ExitCode::from(64))
        }
    }
}

fn hardening_render_network_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut config = PiNetworkHardeningConfig::default();
    let mut output = None;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--output" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--output requires a value");
                    return Err(ExitCode::from(64));
                };
                output = Some(PathBuf::from(value));
                index += 2;
            }
            "--ap-interface" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--ap-interface requires a value");
                    return Err(ExitCode::from(64));
                };
                config.ap_interface = value.to_string();
                index += 2;
            }
            "--ssid" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--ssid requires a value");
                    return Err(ExitCode::from(64));
                };
                config.ssid = value.to_string();
                index += 2;
            }
            "--wpa3-passphrase" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--wpa3-passphrase requires a value");
                    return Err(ExitCode::from(64));
                };
                config.wpa3_passphrase = value.to_string();
                index += 2;
            }
            "--ap-address" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--ap-address requires a value");
                    return Err(ExitCode::from(64));
                };
                let Ok(parsed) = value.parse() else {
                    eprintln!("--ap-address must be an IPv4 address");
                    return Err(ExitCode::from(64));
                };
                config.ap_address = parsed;
                index += 2;
            }
            "--dhcp-start" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--dhcp-start requires a value");
                    return Err(ExitCode::from(64));
                };
                let Ok(parsed) = value.parse() else {
                    eprintln!("--dhcp-start must be an IPv4 address");
                    return Err(ExitCode::from(64));
                };
                config.dhcp_start = parsed;
                index += 2;
            }
            "--dhcp-end" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--dhcp-end requires a value");
                    return Err(ExitCode::from(64));
                };
                let Ok(parsed) = value.parse() else {
                    eprintln!("--dhcp-end must be an IPv4 address");
                    return Err(ExitCode::from(64));
                };
                config.dhcp_end = parsed;
                index += 2;
            }
            "--allow-outbound" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--allow-outbound requires a value");
                    return Err(ExitCode::from(64));
                };
                config
                    .allow_outbound
                    .push(OutboundEndpoint::parse(value).map_err(|error| {
                        eprintln!("invalid --allow-outbound: {error}");
                        ExitCode::from(64)
                    })?);
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let Some(output) = output else {
        eprintln!("hardening render-network requires --output");
        return Err(ExitCode::from(64));
    };
    let written = write_network_hardening_files(&output, &config).map_err(|error| {
        eprintln!("hardening render-network failed: {error}");
        ExitCode::from(78)
    })?;
    for path in written {
        println!("{}", path.display());
    }
    Ok(())
}

fn bundle_command(args: Vec<String>) -> Result<(), ExitCode> {
    let Some(subcommand) = args.first() else {
        eprintln!("usage: kelp-pi-agent bundle <assemble|export> ...");
        return Err(ExitCode::from(64));
    };
    match subcommand.as_str() {
        "assemble" => bundle_assemble_command(args[1..].to_vec()),
        "export" => bundle_export_command(args[1..].to_vec()),
        other => {
            eprintln!("unknown bundle command: {other}");
            Err(ExitCode::from(64))
        }
    }
}

fn bundle_export_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut key_dir = None;
    let mut bundle_id = None;
    let mut run_id = None;
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
            "--bundle-id" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--bundle-id requires a value");
                    return Err(ExitCode::from(64));
                };
                bundle_id = Some(value.to_string());
                index += 2;
            }
            "--run-id" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--run-id requires a value");
                    return Err(ExitCode::from(64));
                };
                run_id = Some(value.to_string());
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let Some(bundle_id) = bundle_id else {
        eprintln!("bundle export requires --bundle-id");
        return Err(ExitCode::from(64));
    };
    let key_dir = key_dir.unwrap_or_else(|| data_dir.join("keys"));
    let identity = load_or_generate_identity_key(&key_dir, DEFAULT_KEY_LABEL).map_err(|error| {
        eprintln!("identity key unavailable: {error}");
        ExitCode::from(78)
    })?;
    let transfer =
        load_pi_bundle_transfer(&data_dir, &bundle_id, run_id.as_deref()).map_err(|error| {
            eprintln!("bundle export failed: {error}");
            ExitCode::from(65)
        })?;
    let payload = serde_json::to_value(&transfer)
        .expect("serialize bundle transfer")
        .as_object()
        .expect("bundle transfer object")
        .clone();
    let envelope = sign_envelope(
        UnsignedPiWireEnvelope {
            msg_id: format!("bundle.export.{}.{}", bundle_id, unix_millis_now()),
            ts: rfc3339_now(),
            sender: PiEnvelopeSender::Pi,
            kind: PiEnvelopeKind::BundleExport,
            payload,
        },
        &identity.signing_key,
    )
    .map_err(|error| {
        eprintln!("bundle export signing failed: {error}");
        ExitCode::from(78)
    })?;
    println!(
        "{}",
        serde_json::to_string(&envelope).expect("serialize bundle export envelope")
    );
    Ok(())
}

fn bundle_assemble_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut key_dir = None;
    let mut workspace = None;
    let mut output = None;
    let mut run_id = None;
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
            "--workspace" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--workspace requires a value");
                    return Err(ExitCode::from(64));
                };
                workspace = Some(PathBuf::from(value));
                index += 2;
            }
            "--output" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--output requires a value");
                    return Err(ExitCode::from(64));
                };
                output = Some(PathBuf::from(value));
                index += 2;
            }
            "--run-id" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--run-id requires a value");
                    return Err(ExitCode::from(64));
                };
                run_id = Some(value.to_string());
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let Some(workspace) = workspace else {
        eprintln!("bundle assemble requires --workspace");
        return Err(ExitCode::from(64));
    };
    let Some(output) = output else {
        eprintln!("bundle assemble requires --output");
        return Err(ExitCode::from(64));
    };
    let Some(run_id) = run_id else {
        eprintln!("bundle assemble requires --run-id");
        return Err(ExitCode::from(64));
    };
    let key_dir = key_dir.unwrap_or_else(|| data_dir.join("keys"));
    match assemble_pi_audit_bundle(&data_dir, &key_dir, &workspace, &output, &run_id) {
        Ok(assembly) => {
            println!(
                "{}",
                serde_json::to_string_pretty(&serde_json::json!({
                    "ok": true,
                    "run_id": assembly.run_id,
                    "bundle_dir": assembly.bundle_dir.display().to_string(),
                    "files": assembly.files,
                    "manifest": assembly.manifest,
                    "manifest_sha256": assembly.manifest_sha256
                }))
                .expect("serialize bundle assembly")
            );
            Ok(())
        }
        Err(error) => {
            eprintln!("bundle assemble failed: {error}");
            Err(ExitCode::from(65))
        }
    }
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

fn policy_command(args: Vec<String>) -> Result<(), ExitCode> {
    let Some(subcommand) = args.first() else {
        eprintln!("usage: kelp-pi-agent policy <pull> ...");
        return Err(ExitCode::from(64));
    };
    match subcommand.as_str() {
        "pull" => policy_pull_command(args[1..].to_vec()),
        other => {
            eprintln!("unknown policy command: {other}");
            Err(ExitCode::from(64))
        }
    }
}

fn outbox_command(args: Vec<String>) -> Result<(), ExitCode> {
    let Some(subcommand) = args.first() else {
        eprintln!("usage: kelp-pi-agent outbox <enqueue|replay> ...");
        return Err(ExitCode::from(64));
    };
    match subcommand.as_str() {
        "enqueue" => outbox_enqueue_command(args[1..].to_vec()),
        "replay" => outbox_replay_command(args[1..].to_vec()),
        other => {
            eprintln!("unknown outbox command: {other}");
            Err(ExitCode::from(64))
        }
    }
}

fn outbox_enqueue_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut key_dir = None;
    let mut kind = None;
    let mut payload_json = None;
    let mut msg_id = None;
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
            "--kind" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--kind requires a value");
                    return Err(ExitCode::from(64));
                };
                kind = Some(value.to_string());
                index += 2;
            }
            "--payload-json" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--payload-json requires a value");
                    return Err(ExitCode::from(64));
                };
                payload_json = Some(value.to_string());
                index += 2;
            }
            "--msg-id" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--msg-id requires a value");
                    return Err(ExitCode::from(64));
                };
                msg_id = Some(value.to_string());
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    init_checked_audit(&data_dir)?;
    let Some(kind) = kind else {
        eprintln!("outbox enqueue requires --kind");
        return Err(ExitCode::from(64));
    };
    let Some(payload_json) = payload_json else {
        eprintln!("outbox enqueue requires --payload-json");
        return Err(ExitCode::from(64));
    };
    let kind = parse_outbox_envelope_kind(&kind).map_err(|error| {
        eprintln!("{error}");
        ExitCode::from(64)
    })?;
    let payload_value: Value = serde_json::from_str(&payload_json).map_err(|error| {
        eprintln!("--payload-json invalid: {error}");
        ExitCode::from(65)
    })?;
    let payload = payload_value.as_object().cloned().ok_or_else(|| {
        eprintln!("--payload-json must be a JSON object");
        ExitCode::from(65)
    })?;
    let key_dir = key_dir.unwrap_or_else(|| data_dir.join("keys"));
    let identity = load_or_generate_identity_key(&key_dir, DEFAULT_KEY_LABEL).map_err(|error| {
        eprintln!("identity key unavailable: {error}");
        ExitCode::from(78)
    })?;
    let msg_id = msg_id.unwrap_or_else(|| format!("outbox.{}", unix_millis_now()));
    let envelope = sign_envelope(
        UnsignedPiWireEnvelope {
            msg_id: msg_id.clone(),
            ts: rfc3339_now(),
            sender: PiEnvelopeSender::Pi,
            kind,
            payload,
        },
        &identity.signing_key,
    )
    .map_err(|error| {
        eprintln!("outbox envelope signing failed: {error}");
        ExitCode::from(78)
    })?;
    let queued_path = enqueue_outbox_envelope(&data_dir, &envelope).map_err(|error| {
        eprintln!("outbox enqueue failed: {error}");
        ExitCode::from(74)
    })?;
    tracing::info!(
        event = "outbox.enqueued",
        msg = "outbox envelope enqueued",
        msg_id = msg_id.as_str(),
        path = %queued_path.display()
    );
    println!(
        "{}",
        serde_json::to_string_pretty(&serde_json::json!({
            "ok": true,
            "msg_id": msg_id,
            "path": queued_path.display().to_string()
        }))
        .expect("serialize outbox enqueue")
    );
    Ok(())
}

fn outbox_replay_command(args: Vec<String>) -> Result<(), ExitCode> {
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

    init_checked_audit(&data_dir)?;
    let replayed = replay_outbox_envelopes(&data_dir).map_err(|error| {
        eprintln!("outbox replay failed: {error}");
        ExitCode::from(74)
    })?;
    tracing::info!(
        event = "outbox.replayed",
        msg = "outbox envelopes replayed",
        msg_id = "outbox-replayed",
        count = replayed
    );
    Ok(())
}

fn parse_outbox_envelope_kind(kind: &str) -> Result<PiEnvelopeKind, String> {
    match kind {
        "policy.pull" => Ok(PiEnvelopeKind::PolicyPull),
        "scan.event" => Ok(PiEnvelopeKind::ScanEvent),
        "scan.complete" => Ok(PiEnvelopeKind::ScanComplete),
        "evidence.append" => Ok(PiEnvelopeKind::EvidenceAppend),
        "bundle.export" => Ok(PiEnvelopeKind::BundleExport),
        "ask.result" => Ok(PiEnvelopeKind::AskResult),
        "selfcheck.report" => Ok(PiEnvelopeKind::SelfcheckReport),
        _ => Err("outbox kind must be a Pi-originated envelope kind".to_string()),
    }
}

fn enqueue_outbox_envelope(
    data_dir: &Path,
    envelope: &PiWireEnvelope,
) -> Result<PathBuf, std::io::Error> {
    let outbox_dir = data_dir.join("outbox");
    let queued_dir = outbox_dir.join("queued");
    fs::create_dir_all(&queued_dir)?;
    let sequence = next_outbox_sequence(&outbox_dir)?;
    let path = queued_dir.join(format!("{sequence:020}.json"));
    let tmp_path = queued_dir.join(format!("{sequence:020}.json.tmp.{}", std::process::id()));
    fs::write(&tmp_path, serde_json::to_vec(envelope)?)?;
    fs::rename(&tmp_path, &path)?;
    Ok(path)
}

fn next_outbox_sequence(outbox_dir: &Path) -> Result<u64, std::io::Error> {
    fs::create_dir_all(outbox_dir)?;
    let path = outbox_dir.join("next-seq");
    let current = match fs::read_to_string(&path) {
        Ok(value) => value.trim().parse::<u64>().unwrap_or(0),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => 0,
        Err(error) => return Err(error),
    };
    let next = current.saturating_add(1);
    let tmp_path = outbox_dir.join(format!("next-seq.tmp.{}", std::process::id()));
    fs::write(&tmp_path, format!("{next}\n"))?;
    fs::rename(&tmp_path, &path)?;
    Ok(next)
}

fn replay_outbox_envelopes(data_dir: &Path) -> Result<usize, std::io::Error> {
    let outbox_dir = data_dir.join("outbox");
    let queued_dir = outbox_dir.join("queued");
    let sent_dir = outbox_dir.join("sent");
    fs::create_dir_all(&queued_dir)?;
    fs::create_dir_all(&sent_dir)?;
    let mut paths = fs::read_dir(&queued_dir)?
        .filter_map(Result::ok)
        .map(|entry| entry.path())
        .filter(|path| path.extension().and_then(|value| value.to_str()) == Some("json"))
        .collect::<Vec<_>>();
    paths.sort();
    let mut replayed = 0_usize;
    for path in paths {
        let bytes = fs::read(&path)?;
        let envelope: PiWireEnvelope = serde_json::from_slice(&bytes)?;
        println!(
            "{}",
            serde_json::to_string(&envelope).expect("serialize outbox envelope")
        );
        let file_name = path.file_name().ok_or_else(|| {
            std::io::Error::new(std::io::ErrorKind::InvalidData, "outbox file has no name")
        })?;
        fs::rename(&path, sent_dir.join(file_name))?;
        replayed += 1;
    }
    Ok(replayed)
}

fn policy_pull_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut key_dir = None;
    let mut device_id = None;
    let mut trust_epoch_override = None;
    let mut request_id = None;
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
            "--device-id" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--device-id requires a value");
                    return Err(ExitCode::from(64));
                };
                device_id = Some(value.to_string());
                index += 2;
            }
            "--trust-epoch" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--trust-epoch requires a value");
                    return Err(ExitCode::from(64));
                };
                let Ok(parsed) = value.parse::<u64>() else {
                    eprintln!("--trust-epoch must be a non-negative integer");
                    return Err(ExitCode::from(64));
                };
                trust_epoch_override = Some(parsed);
                index += 2;
            }
            "--request-id" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--request-id requires a value");
                    return Err(ExitCode::from(64));
                };
                request_id = Some(value.to_string());
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    init_checked_audit(&data_dir)?;
    let key_dir = key_dir.unwrap_or_else(|| data_dir.join("keys"));
    let identity = load_or_generate_identity_key(&key_dir, DEFAULT_KEY_LABEL).map_err(|error| {
        eprintln!("identity key unavailable: {error}");
        ExitCode::from(78)
    })?;
    let current_policy = load_current_policy_pack(&data_dir).map_err(|error| {
        eprintln!("current policy unavailable: {error}");
        ExitCode::from(65)
    })?;
    let known_policy_packs: Vec<Value> = current_policy
        .as_ref()
        .map(|stored| vec![Value::String(stored.payload.policy_pack_id.clone())])
        .unwrap_or_default();
    let trust_epoch = trust_epoch_override
        .or_else(|| {
            current_policy
                .as_ref()
                .map(|stored| stored.payload.trust_epoch)
        })
        .unwrap_or(0);
    let device_id = device_id.unwrap_or_else(|| identity.metadata.label.clone());
    let msg_id =
        request_id.unwrap_or_else(|| format!("policy.pull.{}.{}", device_id, unix_millis_now()));
    let mut payload = Map::new();
    payload.insert("device_id".to_string(), Value::String(device_id.clone()));
    payload.insert(
        "known_policy_packs".to_string(),
        Value::Array(known_policy_packs),
    );
    payload.insert(
        "trust_epoch".to_string(),
        Value::Number(serde_json::Number::from(trust_epoch)),
    );
    tracing::info!(
        event = "policy.pull.requested",
        msg = "policy pull requested",
        msg_id = msg_id.as_str(),
        device_id = device_id.as_str(),
        trust_epoch = trust_epoch
    );
    let envelope = sign_envelope(
        UnsignedPiWireEnvelope {
            msg_id,
            ts: rfc3339_now(),
            sender: PiEnvelopeSender::Pi,
            kind: PiEnvelopeKind::PolicyPull,
            payload,
        },
        &identity.signing_key,
    )
    .map_err(|error| {
        eprintln!("policy.pull signing failed: {error}");
        ExitCode::from(78)
    })?;
    println!(
        "{}",
        serde_json::to_string(&envelope).expect("serialize policy pull envelope")
    );
    Ok(())
}

fn load_current_policy_pack(data_dir: &Path) -> Result<Option<StoredPolicyPack>, std::io::Error> {
    let path = data_dir.join("policy").join(CURRENT_POLICY_FILE);
    match fs::read(&path) {
        Ok(bytes) => serde_json::from_slice(&bytes)
            .map(Some)
            .map_err(|error| std::io::Error::new(std::io::ErrorKind::InvalidData, error)),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => Ok(None),
        Err(error) => Err(error),
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

fn scan_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut scanner = None;
    let mut scanner_bin = None;
    let mut approval_token = None;
    let mut run_id = None;
    let mut dry_run = false;
    let mut enable_zap = false;
    let mut templates_sha = None;
    let mut quota_config_path = None;
    let mut min_free_bytes = None;
    let mut limits = ScannerLimits::default();
    let mut targets = Vec::new();
    let mut scanner_args = Vec::new();
    let mut index = 0;

    if args.first().is_some_and(|value| !value.starts_with('-')) {
        scanner = args.first().cloned();
        index = 1;
    }

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
            "--target" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--target requires a value");
                    return Err(ExitCode::from(64));
                };
                targets.push(value.to_string());
                index += 2;
            }
            "--scanner-bin" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--scanner-bin requires a value");
                    return Err(ExitCode::from(64));
                };
                scanner_bin = Some(value.to_string());
                index += 2;
            }
            "--approval-token" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--approval-token requires a value");
                    return Err(ExitCode::from(64));
                };
                approval_token = Some(value.to_string());
                index += 2;
            }
            "--run-id" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--run-id requires a value");
                    return Err(ExitCode::from(64));
                };
                if !safe_scan_run_id(value) {
                    eprintln!(
                        "--run-id must use only ASCII letters, numbers, dot, dash, or underscore"
                    );
                    return Err(ExitCode::from(64));
                }
                run_id = Some(value.to_string());
                index += 2;
            }
            "--dry-run" => {
                dry_run = true;
                index += 1;
            }
            "--enable-zap" => {
                enable_zap = true;
                index += 1;
            }
            "--templates-sha" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--templates-sha requires a value");
                    return Err(ExitCode::from(64));
                };
                templates_sha = Some(value.to_string());
                index += 2;
            }
            "--quota-config" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--quota-config requires a value");
                    return Err(ExitCode::from(64));
                };
                quota_config_path = Some(PathBuf::from(value));
                index += 2;
            }
            "--min-free-bytes" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--min-free-bytes requires a value");
                    return Err(ExitCode::from(64));
                };
                min_free_bytes = Some(parse_positive_u64("--min-free-bytes", value)?);
                index += 2;
            }
            "--max-requests-per-second" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--max-requests-per-second requires a value");
                    return Err(ExitCode::from(64));
                };
                limits.max_requests_per_second =
                    Some(parse_positive_u32("--max-requests-per-second", value)?);
                index += 2;
            }
            "--max-concurrent-targets" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--max-concurrent-targets requires a value");
                    return Err(ExitCode::from(64));
                };
                limits.max_concurrent_targets =
                    Some(parse_positive_usize("--max-concurrent-targets", value)?);
                index += 2;
            }
            "--max-scan-duration-seconds" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--max-scan-duration-seconds requires a value");
                    return Err(ExitCode::from(64));
                };
                limits.max_scan_duration_seconds =
                    Some(parse_positive_u64("--max-scan-duration-seconds", value)?);
                index += 2;
            }
            "--" => {
                scanner_args.extend(args[index + 1..].iter().cloned());
                break;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let Some(scanner) = scanner else {
        eprintln!("scan requires scanner name: nuclei, nmap, or zap");
        return Err(ExitCode::from(64));
    };
    if !matches!(scanner.as_str(), "nuclei" | "nmap" | "zap") {
        eprintln!("scan scanner must be nuclei, nmap, or zap");
        return Err(ExitCode::from(64));
    }
    limits = limits.validate().map_err(|error| {
        eprintln!("scan limit refused: {error}");
        ExitCode::from(64)
    })?;
    if targets.is_empty() {
        eprintln!("scan requires at least one --target");
        return Err(ExitCode::from(64));
    }

    init_checked_audit(&data_dir)?;
    if let Err(error) = enforce_command_storage_quota(
        &data_dir,
        StorageQuotaScope::Scan,
        quota_config_path.as_deref(),
        min_free_bytes,
    ) {
        audit_storage_quota_refusal(StorageQuotaScope::Scan, &error);
        eprintln!("scan refused by storage quota: {error}");
        return Err(ExitCode::from(75));
    }
    let scope = match load_active_scope(&data_dir) {
        Ok(scope) => scope,
        Err(error) => return refuse_scan_scope(&scanner, &targets, &error.to_string()),
    };
    if let Err(error) = ensure_targets_in_scope(&scope, &targets, unix_millis_now()) {
        return refuse_scan_scope(&scanner, &targets, &scan_scope_error_reason(&error));
    }
    let thermal_guard = evaluate_scan_thermal_guard();
    if thermal_guard.decision == ThermalScanDecision::Refuse {
        tracing::warn!(
            event = "scan.thermal.refused",
            msg = "scan refused by thermal guard",
            msg_id = "scan-thermal-refused",
            scanner = scanner.as_str(),
            scope_id = scope.payload.scope_id.as_str(),
            targets = targets.join(","),
            reason = thermal_guard.reason.as_str(),
            celsius = thermal_guard.status.celsius.unwrap_or(-1.0),
            throttled_flags = thermal_guard.status.throttled_flags.unwrap_or(0) as u64
        );
        eprintln!("scan refused by thermal guard: {}", thermal_guard.reason);
        return Err(ExitCode::from(77));
    }
    if scanner == "zap" {
        let zap_guard = evaluate_zap_guard(enable_zap);
        if zap_guard.decision == ZapDecision::Refuse {
            tracing::warn!(
                event = "scan.zap.refused",
                msg = "ZAP scan refused",
                msg_id = "scan-zap-refused",
                scanner = scanner.as_str(),
                scope_id = scope.payload.scope_id.as_str(),
                targets = targets.join(","),
                reason = zap_guard.reason.as_str(),
                opt_in = zap_guard.status.opt_in,
                raspberry_pi = zap_guard.status.raspberry_pi,
                ram_bytes = zap_guard.status.ram_bytes.unwrap_or(0)
            );
            eprintln!("ZAP scan refused: {}", zap_guard.reason);
            return Err(ExitCode::from(77));
        }
    }
    let nuclei_templates = if scanner == "nuclei" {
        match enforce_nuclei_templates_pin(templates_sha.as_deref()) {
            Ok(pin) => Some(pin),
            Err(error) => {
                tracing::warn!(
                    event = "scan.nuclei.refused",
                    msg = "Nuclei scan refused",
                    msg_id = "scan-nuclei-refused",
                    scanner = scanner.as_str(),
                    scope_id = scope.payload.scope_id.as_str(),
                    targets = targets.join(","),
                    reason = error.to_string().as_str()
                );
                eprintln!("Nuclei scan refused: {error}");
                return Err(ExitCode::from(77));
            }
        }
    } else {
        None
    };
    let scanner_bin = scanner_bin.unwrap_or_else(|| scanner.clone());
    let nmap_version = if scanner == "nmap" {
        match probe_pinned_nmap_version(&scanner_bin) {
            Ok(version) => Some(version),
            Err(error) => {
                tracing::warn!(
                    event = "scan.nmap.refused",
                    msg = "Nmap scan refused",
                    msg_id = "scan-nmap-refused",
                    scanner = scanner.as_str(),
                    scanner_bin = scanner_bin.as_str(),
                    scope_id = scope.payload.scope_id.as_str(),
                    targets = targets.join(","),
                    reason = error.to_string().as_str()
                );
                eprintln!("Nmap scan refused: {error}");
                return Err(ExitCode::from(77));
            }
        }
    } else {
        None
    };
    let enforced_args =
        validate_scanner_limits(&scanner, targets.len(), limits).map_err(|error| {
            tracing::warn!(
                event = "scan.limits.refused",
                msg = "scanner limits refused",
                msg_id = "scan-limits-refused",
                scanner = scanner.as_str(),
                scope_id = scope.payload.scope_id.as_str(),
                targets = targets.join(","),
                reason = error.as_str()
            );
            eprintln!("scan limit refused: {error}");
            ExitCode::from(77)
        })?;

    let command = format!("scan {scanner} {}", targets.join(" "));
    let decision = evaluate_and_audit_local_policy(&PiLocalPolicyRequest {
        gate: PiPolicyGate::ScannerInvocation,
        command: Some(command.clone()),
        path: None,
        host: Some(targets.join(",")),
        mutating: false,
        allowed: true,
    });
    let decision = if let Some(token) = approval_token.as_deref() {
        decision_after_approval(&data_dir, &decision, &scope.payload.scope_id, token).map_err(
            |error| {
                tracing::warn!(
                    event = "scan.policy.refused",
                    msg = "scan refused by policy",
                    msg_id = "scan-policy-refused",
                    scanner = scanner.as_str(),
                    scope_id = scope.payload.scope_id.as_str(),
                    reason = error.to_string().as_str()
                );
                eprintln!("scan approval refused: {error}");
                ExitCode::from(77)
            },
        )?
    } else {
        decision
    };
    if decision.action != PiPolicyAction::Allow {
        tracing::warn!(
            event = "scan.policy.refused",
            msg = "scan refused by policy",
            msg_id = "scan-policy-refused",
            scanner = scanner.as_str(),
            scope_id = scope.payload.scope_id.as_str(),
            action = decision.action.as_str(),
            reason = decision.reason.as_str()
        );
        eprintln!("scan refused by policy: {}", decision.reason);
        return Err(ExitCode::from(77));
    }

    if dry_run {
        println!(
            "{}",
            serde_json::to_string_pretty(&serde_json::json!({
                "status": "ready",
                "scanner": scanner,
                "scanner_bin": scanner_bin,
                "nmap_version": nmap_version.as_ref().map(|version| version.runtime_version.as_str()),
                "nuclei_templates_revision": nuclei_templates.as_ref().map(|pin| pin.revision),
                "limits": {
                    "max_requests_per_second": limits.max_requests_per_second,
                    "max_concurrent_targets": limits.max_concurrent_targets,
                    "max_scan_duration_seconds": limits.max_scan_duration_seconds
                },
                "enforced_args": enforced_args,
                "scope_id": scope.payload.scope_id,
                "targets": targets
            }))
            .expect("serialize scan dry-run")
        );
        return Ok(());
    }

    let started_at = rfc3339_now();
    let run_id = run_id.unwrap_or_else(|| format!("scan-{}", unix_millis_now()));
    let mut run_state = ScanRunState::running(
        run_id,
        scanner.clone(),
        scope.payload.scope_id.clone(),
        targets.clone(),
        started_at,
    );
    write_scan_run_state(&data_dir, &run_state).map_err(|error| {
        eprintln!("scan run state write failed: {error}");
        ExitCode::from(78)
    })?;
    tracing::info!(
        event = "scan.invocation.started",
        msg = "scanner process starting",
        msg_id = "scan-invocation-started",
        scanner = scanner.as_str(),
        scanner_bin = scanner_bin.as_str(),
        scope_id = scope.payload.scope_id.as_str(),
        nmap_version = nmap_version
            .as_ref()
            .map(|version| version.runtime_version.as_str())
            .unwrap_or(""),
        nuclei_templates_revision = nuclei_templates
            .as_ref()
            .map(|pin| pin.revision)
            .unwrap_or(""),
        targets = targets.join(",")
    );
    let outcome =
        match run_scanner_with_limits(&scanner, &scanner_bin, &scanner_args, &targets, limits) {
            Ok(outcome) => outcome,
            Err(error) => {
                tracing::warn!(
                    event = "scan.invocation.failed",
                    msg = "scanner process failed to start",
                    msg_id = "scan-invocation-failed",
                    scanner = scanner.as_str(),
                    scanner_bin = scanner_bin.as_str(),
                    reason = error.to_string().as_str()
                );
                eprintln!("scanner refused: {error}");
                run_state.mark_failed(Some(error.to_string()));
                write_scan_run_state(&data_dir, &run_state).map_err(|state_error| {
                    eprintln!("scan run state write failed: {state_error}");
                    ExitCode::from(78)
                })?;
                return if matches!(error, kelp_pi_agent::ScannerRunError::Limit(_)) {
                    Err(ExitCode::from(77))
                } else {
                    Err(ExitCode::from(69))
                };
            }
        };
    let status = outcome.status;
    if outcome.timed_out {
        run_state.mark_failed(Some(format!(
            "scanner timed out after {} seconds",
            limits.max_scan_duration_seconds.unwrap_or_default()
        )));
    } else if status.success() {
        run_state.mark_succeeded();
    } else {
        run_state.mark_failed(Some(format!("scanner exited with {status}")));
    }
    write_scan_run_state(&data_dir, &run_state).map_err(|error| {
        eprintln!("scan run state write failed: {error}");
        ExitCode::from(78)
    })?;
    tracing::info!(
        event = "scan.invocation.completed",
        msg = "scanner process completed",
        msg_id = "scan-invocation-completed",
        scanner = scanner.as_str(),
        scanner_bin = scanner_bin.as_str(),
        nmap_version = nmap_version
            .as_ref()
            .map(|version| version.runtime_version.as_str())
            .unwrap_or(""),
        nuclei_templates_revision = nuclei_templates
            .as_ref()
            .map(|pin| pin.revision)
            .unwrap_or(""),
        scanner_enforced_args = outcome.enforced_args.join(" "),
        timed_out = outcome.timed_out,
        status = status.code().unwrap_or(-1) as i64
    );
    if outcome.timed_out {
        eprintln!(
            "scanner timed out after {} seconds",
            limits.max_scan_duration_seconds.unwrap_or_default()
        );
        return Err(ExitCode::from(124));
    }
    if status.success() {
        Ok(())
    } else {
        Err(ExitCode::from(65))
    }
}

fn refuse_scan_scope(scanner: &str, targets: &[String], reason: &str) -> Result<(), ExitCode> {
    tracing::warn!(
        event = "scope.violation",
        msg = "scan target refused by scope",
        msg_id = "scope-violation",
        scanner = scanner,
        targets = targets.join(","),
        reason = reason
    );
    eprintln!("scan refused by scope: {reason}");
    Err(ExitCode::from(77))
}

fn scan_scope_error_reason(error: &ScopeError) -> String {
    match error {
        ScopeError::OutOfScope { target, reason } => format!("{target}: {reason}"),
        other => other.to_string(),
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
struct ScanRunState {
    schema_version: String,
    run_id: String,
    scanner: String,
    scope_id: String,
    targets: Vec<String>,
    status: String,
    resumable: bool,
    started_at: String,
    updated_at: String,
    resume_reason: Option<String>,
    error: Option<String>,
}

impl ScanRunState {
    fn running(
        run_id: String,
        scanner: String,
        scope_id: String,
        targets: Vec<String>,
        started_at: String,
    ) -> Self {
        Self {
            schema_version: "kelp.pi.scan-run.v1".to_string(),
            run_id,
            scanner,
            scope_id,
            targets,
            status: "running".to_string(),
            resumable: false,
            updated_at: started_at.clone(),
            started_at,
            resume_reason: None,
            error: None,
        }
    }

    fn mark_succeeded(&mut self) {
        self.status = "succeeded".to_string();
        self.resumable = false;
        self.updated_at = rfc3339_now();
        self.resume_reason = None;
        self.error = None;
    }

    fn mark_failed(&mut self, error: Option<String>) {
        self.status = "failed".to_string();
        self.resumable = false;
        self.updated_at = rfc3339_now();
        self.resume_reason = None;
        self.error = error;
    }

    fn mark_resumable(&mut self) {
        self.status = "resumable".to_string();
        self.resumable = true;
        self.updated_at = rfc3339_now();
        self.resume_reason = Some("previous scanner process ended before completion".to_string());
        self.error = None;
    }
}

fn safe_scan_run_id(value: &str) -> bool {
    !value.is_empty()
        && value.len() <= 128
        && value
            .bytes()
            .all(|byte| byte.is_ascii_alphanumeric() || matches!(byte, b'.' | b'-' | b'_'))
}

fn scan_run_state_path(data_dir: &Path, run_id: &str) -> Result<PathBuf, io::Error> {
    if !safe_scan_run_id(run_id) {
        return Err(io::Error::new(
            io::ErrorKind::InvalidInput,
            "scan run id contains unsafe characters",
        ));
    }
    Ok(data_dir.join("runs").join(format!("{run_id}.json")))
}

fn write_scan_run_state(data_dir: &Path, state: &ScanRunState) -> Result<(), io::Error> {
    let path = scan_run_state_path(data_dir, &state.run_id)?;
    let parent = path.parent().ok_or_else(|| {
        io::Error::new(io::ErrorKind::InvalidInput, "scan run path has no parent")
    })?;
    fs::create_dir_all(parent)?;
    let temp_path = path.with_extension("json.tmp");
    let bytes = serde_json::to_vec_pretty(state)
        .map_err(|error| io::Error::new(io::ErrorKind::InvalidData, error))?;
    fs::write(&temp_path, bytes)?;
    fs::rename(temp_path, path)
}

fn mark_resumable_scan_runs(data_dir: &Path) -> Result<usize, io::Error> {
    let runs_dir = data_dir.join("runs");
    let entries = match fs::read_dir(&runs_dir) {
        Ok(entries) => entries,
        Err(error) if error.kind() == io::ErrorKind::NotFound => return Ok(0),
        Err(error) => return Err(error),
    };
    let mut marked = 0_usize;
    for entry in entries {
        let path = entry?.path();
        if path.extension().and_then(|value| value.to_str()) != Some("json") {
            continue;
        }
        let bytes = fs::read(&path)?;
        let mut state: ScanRunState = serde_json::from_slice(&bytes)
            .map_err(|error| io::Error::new(io::ErrorKind::InvalidData, error))?;
        if state.status != "running" {
            continue;
        }
        state.mark_resumable();
        write_scan_run_state(data_dir, &state)?;
        marked += 1;
        tracing::warn!(
            event = "scan.run.resumable",
            msg = "scan run marked resumable",
            msg_id = "scan-run-resumable",
            run_id = state.run_id.as_str(),
            scanner = state.scanner.as_str(),
            scope_id = state.scope_id.as_str(),
            targets = state.targets.join(",")
        );
    }
    Ok(marked)
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

fn enforce_command_storage_quota(
    data_dir: &Path,
    scope: StorageQuotaScope,
    config_path: Option<&Path>,
    min_free_bytes: Option<u64>,
) -> Result<(), StorageQuotaError> {
    let floor = match min_free_bytes {
        Some(value) => value,
        None => {
            load_storage_quota_config(
                config_path.unwrap_or_else(|| Path::new(DEFAULT_AGENT_CONFIG_PATH)),
            )?
            .min_free_bytes
        }
    };
    enforce_storage_quota(data_dir, scope, floor).map(|_| ())
}

fn audit_storage_quota_refusal(scope: StorageQuotaScope, error: &StorageQuotaError) {
    tracing::warn!(
        event = "storage.quota.refused",
        msg = "storage quota refused operation",
        msg_id = "storage-quota-refused",
        scope = scope.as_str(),
        reason = error.to_string().as_str()
    );
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

fn normalize_command(args: Vec<String>) -> Result<(), ExitCode> {
    let Some(subcommand) = args.first() else {
        eprintln!(
            "usage: kelp-pi-agent normalize nuclei --input PATH --workspace PATH [--raw-path PATH] [--data-dir PATH] [--quota-config PATH] [--min-free-bytes N]"
        );
        return Err(ExitCode::from(64));
    };
    match subcommand.as_str() {
        "nuclei" => normalize_nuclei_command(args[1..].to_vec()),
        other => {
            eprintln!("unknown normalize command: {other}");
            Err(ExitCode::from(64))
        }
    }
}

fn normalize_nuclei_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = None;
    let mut quota_config_path = None;
    let mut min_free_bytes = None;
    let mut input = None;
    let mut workspace = None;
    let mut raw_path = None;
    let mut index = 0;

    while index < args.len() {
        match args[index].as_str() {
            "--data-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--data-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                data_dir = Some(PathBuf::from(value));
                index += 2;
            }
            "--quota-config" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--quota-config requires a value");
                    return Err(ExitCode::from(64));
                };
                quota_config_path = Some(PathBuf::from(value));
                index += 2;
            }
            "--min-free-bytes" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--min-free-bytes requires a value");
                    return Err(ExitCode::from(64));
                };
                min_free_bytes = Some(parse_positive_u64("--min-free-bytes", value)?);
                index += 2;
            }
            "--input" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--input requires a value");
                    return Err(ExitCode::from(64));
                };
                input = Some(PathBuf::from(value));
                index += 2;
            }
            "--workspace" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--workspace requires a value");
                    return Err(ExitCode::from(64));
                };
                workspace = Some(PathBuf::from(value));
                index += 2;
            }
            "--raw-path" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--raw-path requires a value");
                    return Err(ExitCode::from(64));
                };
                raw_path = Some(value.to_string());
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let Some(input) = input else {
        eprintln!("normalize nuclei requires --input");
        return Err(ExitCode::from(64));
    };
    let Some(workspace) = workspace else {
        eprintln!("normalize nuclei requires --workspace");
        return Err(ExitCode::from(64));
    };
    if (quota_config_path.is_some() || min_free_bytes.is_some()) && data_dir.is_none() {
        eprintln!("normalize nuclei quota options require --data-dir");
        return Err(ExitCode::from(64));
    }
    if let Some(data_dir) = data_dir.as_deref() {
        init_checked_audit(data_dir)?;
        if let Err(error) = enforce_command_storage_quota(
            data_dir,
            StorageQuotaScope::Ingest,
            quota_config_path.as_deref(),
            min_free_bytes,
        ) {
            audit_storage_quota_refusal(StorageQuotaScope::Ingest, &error);
            eprintln!("normalize nuclei refused by storage quota: {error}");
            return Err(ExitCode::from(75));
        }
    }
    let raw_path = raw_path.unwrap_or_else(|| {
        input
            .file_name()
            .and_then(|name| name.to_str())
            .unwrap_or("nuclei.jsonl")
            .to_string()
    });
    let output = workspace.join("normalized").join("findings.json");
    match write_nuclei_findings_document(&input, &output, &raw_path) {
        Ok(document) => {
            println!(
                "{}",
                serde_json::to_string_pretty(&serde_json::json!({
                    "ok": true,
                    "format": "nuclei",
                    "findings": document.findings.len(),
                    "output": output.display().to_string()
                }))
                .expect("serialize normalize summary")
            );
            Ok(())
        }
        Err(error) => {
            eprintln!("normalize nuclei failed: {error}");
            Err(ExitCode::from(65))
        }
    }
}

fn upload_command(args: Vec<String>) -> Result<(), ExitCode> {
    let Some(subcommand) = args.first() else {
        eprintln!("usage: kelp-pi-agent upload accept --input PATH [--name NAME] [--data-dir PATH] [--quota-config PATH] [--min-free-bytes N]");
        return Err(ExitCode::from(64));
    };
    match subcommand.as_str() {
        "accept" => upload_accept_command(args[1..].to_vec()),
        other => {
            eprintln!("unknown upload command: {other}");
            Err(ExitCode::from(64))
        }
    }
}

fn upload_accept_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut quota_config_path = None;
    let mut min_free_bytes = None;
    let mut input = None;
    let mut name = None;
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
            "--quota-config" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--quota-config requires a value");
                    return Err(ExitCode::from(64));
                };
                quota_config_path = Some(PathBuf::from(value));
                index += 2;
            }
            "--min-free-bytes" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--min-free-bytes requires a value");
                    return Err(ExitCode::from(64));
                };
                min_free_bytes = Some(parse_positive_u64("--min-free-bytes", value)?);
                index += 2;
            }
            "--input" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--input requires a value");
                    return Err(ExitCode::from(64));
                };
                input = Some(PathBuf::from(value));
                index += 2;
            }
            "--name" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--name requires a value");
                    return Err(ExitCode::from(64));
                };
                name = Some(value.to_string());
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let Some(input) = input else {
        eprintln!("upload accept requires --input");
        return Err(ExitCode::from(64));
    };
    let upload_name = match name {
        Some(value) => safe_upload_name(&value)?,
        None => input
            .file_name()
            .and_then(|value| value.to_str())
            .ok_or_else(|| {
                eprintln!("upload accept input must have a file name or --name");
                ExitCode::from(64)
            })
            .and_then(safe_upload_name)?,
    };

    init_checked_audit(&data_dir)?;
    if let Err(error) = enforce_command_storage_quota(
        &data_dir,
        StorageQuotaScope::Upload,
        quota_config_path.as_deref(),
        min_free_bytes,
    ) {
        audit_storage_quota_refusal(StorageQuotaScope::Upload, &error);
        eprintln!("upload accept refused by storage quota: {error}");
        return Err(ExitCode::from(75));
    }

    let upload_dir = data_dir.join("evidence").join("uploads");
    if let Err(error) = fs::create_dir_all(&upload_dir) {
        eprintln!("upload accept failed to create upload dir: {error}");
        return Err(ExitCode::from(74));
    }
    let output = upload_dir.join(&upload_name);
    let size_bytes = match fs::copy(&input, &output) {
        Ok(size) => size,
        Err(error) => {
            eprintln!("upload accept failed: {error}");
            return Err(ExitCode::from(74));
        }
    };
    tracing::info!(
        event = "upload.accepted",
        msg = "upload accepted",
        msg_id = "upload-accepted",
        name = upload_name.as_str(),
        size_bytes = size_bytes
    );
    println!(
        "{}",
        serde_json::to_string_pretty(&serde_json::json!({
            "ok": true,
            "name": upload_name,
            "path": output.display().to_string(),
            "size_bytes": size_bytes
        }))
        .expect("serialize upload response")
    );
    Ok(())
}

fn safe_upload_name(value: &str) -> Result<String, ExitCode> {
    if value.is_empty()
        || value == "."
        || value == ".."
        || value.contains('/')
        || value.contains('\\')
    {
        eprintln!("upload name must be a single safe file name");
        return Err(ExitCode::from(64));
    }
    Ok(value.to_string())
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

fn firmware_update_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut bundle_dir = None;
    let mut trusted_public_key_hex = None;
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
            "--bundle-dir" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--bundle-dir requires a value");
                    return Err(ExitCode::from(64));
                };
                bundle_dir = Some(PathBuf::from(value));
                index += 2;
            }
            "--trusted-public-key-hex" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--trusted-public-key-hex requires a value");
                    return Err(ExitCode::from(64));
                };
                trusted_public_key_hex = Some(value.to_string());
                index += 2;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    let Some(bundle_dir) = bundle_dir else {
        eprintln!("firmware-update requires --bundle-dir");
        return Err(ExitCode::from(64));
    };
    let Some(trusted_public_key_hex) = trusted_public_key_hex else {
        eprintln!("firmware-update requires --trusted-public-key-hex");
        return Err(ExitCode::from(64));
    };
    let trusted_key = verifying_key_from_hex(&trusted_public_key_hex).map_err(|error| {
        eprintln!("invalid trusted public key: {error}");
        ExitCode::from(64)
    })?;

    init_checked_audit(&data_dir)?;

    match verify_and_stage_firmware_update(&data_dir, &bundle_dir, &trusted_key) {
        Ok(receipt) => {
            tracing::info!(
                event = "firmware.update.staged",
                msg = "firmware update staged",
                msg_id = "firmware-update-staged",
                update_id = receipt.update_id.as_str(),
                version = receipt.version.as_str(),
                staged_dir = receipt.staged_dir.as_str()
            );
            println!(
                "{}",
                serde_json::to_string_pretty(&receipt).expect("serialize firmware update receipt")
            );
            Ok(())
        }
        Err(error) => {
            tracing::warn!(
                event = "firmware.update.refused",
                msg = "firmware update refused",
                msg_id = "firmware-update-refused",
                bundle_dir = %bundle_dir.display(),
                reason = error.to_string().as_str()
            );
            eprintln!("firmware update refused: {error}");
            Err(ExitCode::from(65))
        }
    }
}

fn wipe_command(args: Vec<String>) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut force = false;
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
            "--force" => {
                force = true;
                index += 1;
            }
            other => {
                eprintln!("unknown argument: {other}");
                return Err(ExitCode::from(64));
            }
        }
    }

    if !force {
        eprintln!("wipe requires --force");
        return Err(ExitCode::from(64));
    }

    match wipe_data_dir(&data_dir) {
        Ok(report) => {
            println!(
                "{}",
                serde_json::to_string_pretty(&report).expect("serialize wipe report")
            );
            Ok(())
        }
        Err(error) => {
            eprintln!("wipe failed: {error}");
            Err(ExitCode::from(65))
        }
    }
}

fn check_data_dir(args: Vec<String>, start_mode: bool) -> Result<(), ExitCode> {
    let mut data_dir = PathBuf::from(DEFAULT_DATA_DIR);
    let mut check_only = false;
    let mut policy_push_file = None;
    let mut policy_pull_interval_seconds = 300_u64;
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
            "--check-only" => {
                check_only = true;
                index += 1;
            }
            "--policy-push-file" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--policy-push-file requires a value");
                    return Err(ExitCode::from(64));
                };
                policy_push_file = Some(PathBuf::from(value));
                index += 2;
            }
            "--policy-pull-interval-seconds" => {
                let Some(value) = args.get(index + 1) else {
                    eprintln!("--policy-pull-interval-seconds requires a value");
                    return Err(ExitCode::from(64));
                };
                let Ok(parsed) = value.parse::<u64>() else {
                    eprintln!("--policy-pull-interval-seconds must be a positive integer");
                    return Err(ExitCode::from(64));
                };
                if parsed == 0 {
                    eprintln!("--policy-pull-interval-seconds must be a positive integer");
                    return Err(ExitCode::from(64));
                }
                policy_pull_interval_seconds = parsed;
                index += 2;
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
        if let Err(error) = mark_resumable_scan_runs(&data_dir) {
            eprintln!("scan run resume check failed: {error}");
            return Err(ExitCode::from(78));
        }
        Some(panic_hook)
    } else {
        None
    };

    let policy_sync = start_policy_sync_config(
        policy_push_file,
        trusted_cp_public_key_hex,
        policy_pull_interval_seconds,
    )?;

    if start_mode && !check_only {
        return run_daemon(&data_dir, policy_sync);
    }

    println!("data dir ok: {}", data_dir.display());
    Ok(())
}

#[derive(Debug, Clone)]
struct StartPolicySyncConfig {
    policy_push_file: PathBuf,
    trusted_keys: Vec<TrustedControlPlaneKey>,
    interval: Duration,
}

#[derive(Debug, Default)]
struct PolicySyncState {
    last_msg_id: Option<String>,
}

fn start_policy_sync_config(
    policy_push_file: Option<PathBuf>,
    trusted_cp_public_key_hex: Option<String>,
    interval_seconds: u64,
) -> Result<Option<StartPolicySyncConfig>, ExitCode> {
    match (policy_push_file, trusted_cp_public_key_hex) {
        (None, None) => Ok(None),
        (Some(_), None) => {
            eprintln!("--policy-push-file requires --trusted-cp-public-key-hex");
            Err(ExitCode::from(64))
        }
        (None, Some(_)) => {
            eprintln!("--trusted-cp-public-key-hex requires --policy-push-file");
            Err(ExitCode::from(64))
        }
        (Some(policy_push_file), Some(public_key_raw_hex)) => {
            let key_id = trusted_key_id_from_raw_hex(&public_key_raw_hex).map_err(|error| {
                eprintln!("invalid trusted control-plane key: {error}");
                ExitCode::from(64)
            })?;
            Ok(Some(StartPolicySyncConfig {
                policy_push_file,
                trusted_keys: vec![TrustedControlPlaneKey {
                    key_id,
                    public_key_raw_hex,
                    state: PolicyTrustState::Trusted,
                }],
                interval: Duration::from_secs(interval_seconds),
            }))
        }
    }
}

fn run_daemon(data_dir: &Path, policy_sync: Option<StartPolicySyncConfig>) -> Result<(), ExitCode> {
    let runtime = tokio::runtime::Builder::new_current_thread()
        .enable_all()
        .build()
        .map_err(|error| {
            eprintln!("daemon runtime init failed: {error}");
            ExitCode::from(69)
        })?;
    runtime.block_on(wait_for_shutdown(data_dir, policy_sync))
}

#[cfg(unix)]
async fn wait_for_shutdown(
    data_dir: &Path,
    policy_sync: Option<StartPolicySyncConfig>,
) -> Result<(), ExitCode> {
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
    let policy_sync_task =
        policy_sync.map(|config| spawn_policy_sync_loop(data_dir.to_path_buf(), config));
    let shutdown_signal = tokio::select! {
        _ = sigterm.recv() => "SIGTERM",
        _ = sigint.recv() => "SIGINT",
    };
    if let Some(task) = policy_sync_task {
        task.abort();
    }
    tracing::info!(
        event = "agent.daemon.stopped",
        msg = "daemon stopped",
        msg_id = "agent-daemon-stopped",
        shutdown_signal = shutdown_signal
    );
    Ok(())
}

#[cfg(not(unix))]
async fn wait_for_shutdown(
    data_dir: &Path,
    policy_sync: Option<StartPolicySyncConfig>,
) -> Result<(), ExitCode> {
    tracing::info!(
        event = "agent.daemon.started",
        msg = "daemon started",
        msg_id = "agent-daemon-started",
        data_dir = %data_dir.display()
    );
    let policy_sync_task =
        policy_sync.map(|config| spawn_policy_sync_loop(data_dir.to_path_buf(), config));
    tokio::signal::ctrl_c().await.map_err(|error| {
        eprintln!("shutdown handler failed: {error}");
        ExitCode::from(69)
    })?;
    if let Some(task) = policy_sync_task {
        task.abort();
    }
    tracing::info!(
        event = "agent.daemon.stopped",
        msg = "daemon stopped",
        msg_id = "agent-daemon-stopped",
        shutdown_signal = "ctrl_c"
    );
    Ok(())
}

fn spawn_policy_sync_loop(
    data_dir: PathBuf,
    config: StartPolicySyncConfig,
) -> tokio::task::JoinHandle<()> {
    tokio::spawn(async move {
        let mut state = PolicySyncState::default();
        policy_sync_tick(&data_dir, &config, &mut state).await;
        loop {
            tokio::time::sleep(config.interval).await;
            policy_sync_tick(&data_dir, &config, &mut state).await;
        }
    })
}

async fn policy_sync_tick(
    data_dir: &Path,
    config: &StartPolicySyncConfig,
    state: &mut PolicySyncState,
) {
    let msg_id = format!("policy.pull.daemon.{}", unix_millis_now());
    tracing::info!(
        event = "policy.pull.requested",
        msg = "policy pull requested",
        msg_id = msg_id.as_str(),
        trust_source = %config.policy_push_file.display()
    );
    let bytes = match tokio::fs::read(&config.policy_push_file).await {
        Ok(bytes) => bytes,
        Err(error) => {
            tracing::warn!(
                event = "policy.pull.failed",
                msg = "policy pull failed",
                msg_id = msg_id.as_str(),
                reason = error.to_string().as_str()
            );
            return;
        }
    };
    let envelope: PiWireEnvelope = match serde_json::from_slice(&bytes) {
        Ok(envelope) => envelope,
        Err(error) => {
            tracing::warn!(
                event = "policy.pull.failed",
                msg = "policy pull failed",
                msg_id = msg_id.as_str(),
                reason = error.to_string().as_str()
            );
            return;
        }
    };
    if state.last_msg_id.as_deref() == Some(envelope.msg_id.as_str()) {
        return;
    }
    match apply_and_audit_policy_push(data_dir, &envelope, &config.trusted_keys) {
        Ok(_) => {
            state.last_msg_id = Some(envelope.msg_id);
        }
        Err(error) => {
            tracing::warn!(
                event = "policy.push.refused",
                msg = "policy push refused",
                msg_id = msg_id.as_str(),
                reason = error.as_str()
            );
        }
    }
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

fn trusted_key_id_from_raw_hex(hex: &str) -> Result<String, String> {
    let bytes = decode_hex(hex)?;
    if bytes.len() != 32 {
        return Err("public key must be 32 bytes".to_string());
    }
    Ok(format!("sha256:{}", encode_hex(Sha256::digest(bytes))))
}

fn encode_hex(bytes: impl AsRef<[u8]>) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let bytes = bytes.as_ref();
    let mut encoded = String::with_capacity(bytes.len() * 2);
    for byte in bytes {
        encoded.push(HEX[(byte >> 4) as usize] as char);
        encoded.push(HEX[(byte & 0x0f) as usize] as char);
    }
    encoded
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
    eprintln!("usage: kelp-pi-agent bundle assemble --run-id ID --workspace PATH --output PATH [--data-dir PATH] [--key-dir PATH]");
    eprintln!("usage: kelp-pi-agent bundle export --bundle-id ID [--run-id ID] [--data-dir PATH] [--key-dir PATH]");
    eprintln!("usage: kelp-pi-agent check-data-dir [--data-dir PATH]");
    eprintln!("usage: kelp-pi-agent doctor [--data-dir PATH]");
    eprintln!(
        "usage: kelp-pi-agent eval <gold|synthesis|chunk-ids|scanner-stability> [--fixture-dir PATH] [--top-k N] [--no-answer-threshold FLOAT]"
    );
    eprintln!(
        "usage: kelp-pi-agent firmware-update --bundle-dir PATH --trusted-public-key-hex HEX [--data-dir PATH]"
    );
    eprintln!(
        "usage: kelp-pi-agent hardening render-network --output DIR --wpa3-passphrase PASS [--ssid SSID] [--ap-interface IFACE] [--ap-address IPv4] [--dhcp-start IPv4] [--dhcp-end IPv4] [--allow-outbound HOST:PORT]"
    );
    eprintln!("usage: kelp-pi-agent keygen [--data-dir PATH] [--key-dir PATH] [--label LABEL]");
    eprintln!(
        "usage: kelp-pi-agent normalize nuclei --input PATH --workspace PATH [--raw-path PATH] [--data-dir PATH] [--quota-config PATH] [--min-free-bytes N]"
    );
    eprintln!(
        "usage: kelp-pi-agent outbox enqueue --kind KIND --payload-json JSON [--msg-id ID] [--data-dir PATH] [--key-dir PATH]"
    );
    eprintln!("usage: kelp-pi-agent outbox replay [--data-dir PATH]");
    eprintln!(
        "usage: kelp-pi-agent policy pull [--data-dir PATH] [--key-dir PATH] [--device-id ID] [--trust-epoch N] [--request-id ID]"
    );
    eprintln!(
        "usage: kelp-pi-agent policy-check --gate GATE [--mode enforce|dry-run] [--dry-run] [--command CMD] [--path PATH] [--host HOST] [--mutating] [--allowed|--disallowed] [--data-dir PATH]"
    );
    eprintln!("usage: kelp-pi-agent quota-defaults");
    eprintln!("usage: kelp-pi-agent rotate-audit-log [--data-dir PATH] [--key-dir PATH]");
    eprintln!(
        "usage: kelp-pi-agent scan <nuclei|nmap|zap> --target TARGET [--target TARGET...] [--scanner-bin PATH] [--approval-token TOKEN] [--run-id ID] [--dry-run] [--enable-zap] [--templates-sha SHA] [--quota-config PATH] [--min-free-bytes N] [--max-requests-per-second N] [--max-concurrent-targets N] [--max-scan-duration-seconds N] [--data-dir PATH] [-- SCANNER_ARG...]"
    );
    eprintln!(
        "usage: kelp-pi-agent serve-ask [--data-dir PATH] [--db PATH] [--bind IP:PORT] [--top-k N] [--no-answer-threshold FLOAT] [--max-concurrent N] [--rate-limit-per-minute N] [--allow-non-loopback]"
    );
    eprintln!(
        "usage: kelp-pi-agent selfcheck [--data-dir PATH] [--target TARGET] [--signed-envelope] [--check-id ID]"
    );
    eprintln!(
        "usage: kelp-pi-agent start [--data-dir PATH] [--check-only] [--policy-push-file PATH --trusted-cp-public-key-hex HEX --policy-pull-interval-seconds N]"
    );
    eprintln!(
        "usage: kelp-pi-agent upload accept --input PATH [--name NAME] [--data-dir PATH] [--quota-config PATH] [--min-free-bytes N]"
    );
    eprintln!("usage: kelp-pi-agent version");
    eprintln!(
        "usage: kelp-pi-agent verify-audit-log [--data-dir PATH] [--key-dir PATH] [--log-file PATH]"
    );
    eprintln!("usage: kelp-pi-agent wipe --force [--data-dir PATH]");
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

fn parse_positive_u32(flag: &str, value: &str) -> Result<u32, ExitCode> {
    let Ok(parsed) = value.parse::<u32>() else {
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
        "{{\"corpus_bytes\":{},\"uploads_bytes\":{},\"index_bytes\":{},\"audit_log_bytes\":{},\"min_free_bytes\":{}}}",
        DEFAULT_QUOTAS.corpus_bytes,
        DEFAULT_QUOTAS.uploads_bytes,
        DEFAULT_QUOTAS.index_bytes,
        DEFAULT_QUOTAS.audit_log_bytes,
        DEFAULT_QUOTAS.min_free_bytes
    );
}
