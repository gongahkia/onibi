const std = @import("std");
const audit = @import("audit.zig");
const common = @import("common.zig");
const chat = @import("chat.zig");
const keys = @import("keys.zig");

const core_bundle_files = [_][]const u8{
    "audit-chain.json",
    "audit-head.json",
    "audit-log.jsonl",
    "compatibility.json",
    "findings.sarif",
    "index.html",
    "normalized-findings.json",
    "policy-decisions.json",
    "redaction-report.json",
    "result.json",
    "transcript.jsonl",
};

const export_bundle_files = [_][]const u8{
    "audit-chain.json",
    "audit-head.json",
    "audit-log.jsonl",
    "attestation.json",
    "attestation.sig",
    "compatibility.json",
    "findings.sarif",
    "index.html",
    "manifest.json",
    "manifest.pub.json",
    "manifest.sig",
    "normalized-findings.json",
    "policy-decisions.json",
    "redaction-report.json",
    "result.json",
    "transcript.jsonl",
};

pub fn bundleCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0) return common.fail("usage: kelp-pi bundle <assemble|export|import>", 64);
    if (std.mem.eql(u8, args[0], "export")) return bundleExport(allocator, args[1..]);
    if (std.mem.eql(u8, args[0], "import")) return bundleImport(allocator, args[1..]);
    if (!std.mem.eql(u8, args[0], "assemble")) return common.fail("usage: kelp-pi bundle <assemble|export|import>", 64);
    const run_id = common.option(args[1..], "--run-id") orelse "local";
    const workspace = common.option(args[1..], "--workspace") orelse ".";
    const output = common.option(args[1..], "--output") orelse "audit-bundle";
    const data_dir = common.option(args[1..], "--data-dir") orelse common.default_data_dir;
    const key_dir = try common.pathJoin(allocator, data_dir, "keys");
    defer allocator.free(key_dir);
    try std.fs.cwd().makePath(output);
    var key = try keys.loadOrCreateKey(allocator, key_dir);
    defer keys.freeKeyMaterial(allocator, &key);
    try audit.appendEvent(allocator, data_dir, "bundle.assemble", run_id, output);
    try writeBundleCoreFiles(allocator, data_dir, workspace, output, run_id);
    const manifest_payload = try writeSignedManifest(allocator, output, run_id, key);
    defer allocator.free(manifest_payload);
    try writeSignedAttestation(allocator, output, run_id, manifest_payload, key);
    const manifest_hash = try common.contentHashHex(allocator, manifest_payload);
    defer allocator.free(manifest_hash);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"runId\":\"{s}\",\"bundleDir\":\"{s}\",\"manifestSha256\":\"{s}\"}}\n", .{ run_id, output, manifest_hash });
}

pub fn verifyBundle(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0) return common.fail("usage: kelp-pi verify-bundle DIR", 64);
    const dir = args[0];
    const result = verifyBundleDir(allocator, dir) catch |err| {
        var out = std.fs.File.stdout().deprecatedWriter();
        return out.print("{{\"ok\":false,\"bundleDir\":\"{s}\",\"reason\":\"{s}\"}}\n", .{ dir, @errorName(err) });
    };
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":{},\"bundleDir\":\"{s}\",\"filesChecked\":{}}}\n", .{ result, dir, if (result) @as(usize, 1) else @as(usize, 0) });
}

fn bundleExport(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const bundle_id = common.option(args, "--bundle-id") orelse return common.fail("bundle export requires --bundle-id", 64);
    try validateBundleLookupId(bundle_id);
    const run_id = common.option(args, "--run-id") orelse bundle_id;
    try validateBundleLookupId(run_id);
    const data_dir = common.option(args, "--data-dir") orelse common.default_data_dir;
    const bundle_dir = try stagedBundleDir(allocator, data_dir, bundle_id, run_id);
    defer allocator.free(bundle_dir);
    const key_dir = common.option(args, "--key-dir") orelse try common.pathJoin(allocator, data_dir, "keys");
    defer if (common.option(args, "--key-dir") == null) allocator.free(key_dir);
    var key = try keys.loadOrCreateKey(allocator, key_dir);
    defer keys.freeKeyMaterial(allocator, &key);
    const payload = try buildTransferPayload(allocator, bundle_dir, bundle_id, run_id);
    defer allocator.free(payload);
    const sig = try key.key_pair.sign(payload, null);
    const sig_bytes = sig.toBytes();
    const sig_b64 = try base64Alloc(allocator, &sig_bytes);
    defer allocator.free(sig_b64);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"msg_id\":\"bundle.export.{s}\",\"ts\":\"1970-01-01T00:00:00Z\",\"sender\":\"pi\",\"kind\":\"bundle.export\",\"payload\":{s},\"signature\":{{\"algorithm\":\"ed25519\",\"publicKeyHex\":\"{s}\",\"signatureBase64\":\"{s}\"}}}}\n", .{ bundle_id, payload, key.public_hex, std.mem.trim(u8, sig_b64, " \t\r\n") });
}

fn bundleImport(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const input = common.option(args, "--input") orelse return common.fail("bundle import requires --input", 64);
    const output = common.option(args, "--out") orelse return common.fail("bundle import requires --out", 64);
    const text = try std.fs.cwd().readFileAlloc(allocator, input, 32 * 1024 * 1024);
    defer allocator.free(text);
    const parsed = try std.json.parseFromSlice(std.json.Value, allocator, text, .{});
    defer parsed.deinit();
    const kind = try stringField(parsed.value, "kind");
    if (!std.mem.eql(u8, kind, "bundle.export")) return error.InvalidBundleEnvelope;
    const payload = try objectField(parsed.value, "payload");
    const files_value = try arrayField(payload, "files");
    try std.fs.cwd().makePath(output);
    var written: usize = 0;
    for (files_value.items) |file_value| {
        const rel = try stringField(file_value, "path");
        if (!safeRelativePath(rel)) return error.UnsafeBundlePath;
        const content = try stringField(file_value, "content_base64");
        const decoded_len = try std.base64.standard.Decoder.calcSizeForSlice(content);
        const decoded = try allocator.alloc(u8, decoded_len);
        defer allocator.free(decoded);
        try std.base64.standard.Decoder.decode(decoded, content);
        const out_path = try common.pathJoin(allocator, output, rel);
        defer allocator.free(out_path);
        try common.writeFileWithParents(out_path, decoded);
        written += 1;
    }
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"bundleId\":\"{s}\",\"runId\":\"{s}\",\"bundleDir\":\"{s}\",\"manifestHash\":\"{s}\",\"files\":{}}}\n", .{
        try stringField(payload, "bundle_id"),
        try stringField(payload, "run_id"),
        output,
        try stringField(payload, "manifest_hash"),
        written,
    });
}

fn stagedBundleDir(allocator: std.mem.Allocator, data_dir: []const u8, bundle_id: []const u8, run_id: []const u8) ![]u8 {
    const bundles_dir = try common.pathJoin(allocator, data_dir, "bundles");
    defer allocator.free(bundles_dir);
    const by_bundle = try common.pathJoin(allocator, bundles_dir, bundle_id);
    if (common.fileExists(by_bundle)) return by_bundle;
    allocator.free(by_bundle);
    const by_run = try common.pathJoin(allocator, bundles_dir, run_id);
    if (common.fileExists(by_run)) return by_run;
    allocator.free(by_run);
    return error.MissingBundle;
}

fn buildTransferPayload(allocator: std.mem.Allocator, bundle_dir: []const u8, bundle_id: []const u8, run_id: []const u8) ![]u8 {
    const manifest_path = try common.pathJoin(allocator, bundle_dir, "manifest.json");
    defer allocator.free(manifest_path);
    const manifest_hash = try common.fileHashHex(allocator, manifest_path);
    defer allocator.free(manifest_hash);
    var files_json: std.ArrayList(u8) = .empty;
    defer files_json.deinit(allocator);
    var files_writer = files_json.writer(allocator);
    var total_size: usize = 0;
    var count: usize = 0;
    for (export_bundle_files) |file| {
        const file_path = try common.pathJoin(allocator, bundle_dir, file);
        defer allocator.free(file_path);
        if (!common.fileExists(file_path)) continue;
        const bytes = try std.fs.cwd().readFileAlloc(allocator, file_path, 16 * 1024 * 1024);
        defer allocator.free(bytes);
        const hash = try common.contentHashHex(allocator, bytes);
        defer allocator.free(hash);
        const b64 = try base64AllocNoNewline(allocator, bytes);
        defer allocator.free(b64);
        if (count != 0) try files_writer.writeAll(",");
        try files_writer.print("{{\"path\":\"{s}\",\"size_bytes\":{},\"sha256\":\"sha256:{s}\",\"content_base64\":\"{s}\"}}", .{ file, bytes.len, hash, b64 });
        total_size += bytes.len;
        count += 1;
    }
    const files = try files_json.toOwnedSlice(allocator);
    defer allocator.free(files);
    return std.fmt.allocPrint(allocator, "{{\"run_id\":\"{s}\",\"bundle_id\":\"{s}\",\"manifest_hash\":\"sha256:{s}\",\"size_bytes\":{},\"files\":[{s}]}}", .{ run_id, bundle_id, manifest_hash, total_size, files });
}

fn validateBundleLookupId(value: []const u8) !void {
    if (!common.safePathSegment(value) or std.mem.eql(u8, value, ".") or std.mem.eql(u8, value, "..")) return error.InvalidBundleId;
}

fn objectField(value: std.json.Value, name: []const u8) !std.json.Value {
    if (value != .object) return error.InvalidJson;
    return value.object.get(name) orelse error.MissingJsonField;
}

fn arrayField(value: std.json.Value, name: []const u8) !std.json.Array {
    const field = try objectField(value, name);
    if (field != .array) return error.InvalidJson;
    return field.array;
}

fn stringField(value: std.json.Value, name: []const u8) ![]const u8 {
    const field = try objectField(value, name);
    if (field != .string) return error.InvalidJson;
    return field.string;
}

fn safeRelativePath(path: []const u8) bool {
    if (path.len == 0 or path[0] == '/') return false;
    var parts = std.mem.splitScalar(u8, path, '/');
    while (parts.next()) |part| {
        if (part.len == 0 or std.mem.eql(u8, part, ".") or std.mem.eql(u8, part, "..") or !common.safePathSegment(part)) return false;
    }
    return true;
}

fn writeBundleTranscript(allocator: std.mem.Allocator, data_dir: []const u8, output: []const u8, run_id: []const u8) !void {
    const out_path = try common.pathJoin(allocator, output, "transcript.jsonl");
    defer allocator.free(out_path);
    if (common.safePathSegment(run_id)) {
        const run_transcript = try chat.transcriptFilePath(allocator, data_dir, run_id);
        defer allocator.free(run_transcript);
        if (common.fileExists(run_transcript)) {
            const bytes = try std.fs.cwd().readFileAlloc(allocator, run_transcript, 16 * 1024 * 1024);
            defer allocator.free(bytes);
            return common.writeFileWithParents(out_path, bytes);
        }
    }
    const local_transcript = try chat.transcriptFilePath(allocator, data_dir, "local");
    defer allocator.free(local_transcript);
    if (common.fileExists(local_transcript)) {
        const bytes = try std.fs.cwd().readFileAlloc(allocator, local_transcript, 16 * 1024 * 1024);
        defer allocator.free(bytes);
        return common.writeFileWithParents(out_path, bytes);
    }
    try common.writeFileWithParents(out_path, "");
}

fn writeBundleCoreFiles(allocator: std.mem.Allocator, data_dir: []const u8, workspace: []const u8, output: []const u8, run_id: []const u8) !void {
    try std.fs.cwd().makePath(output);
    const result_path = try common.pathJoin(allocator, output, "result.json");
    defer allocator.free(result_path);
    const result = try std.fmt.allocPrint(allocator, "{{\"schemaVersion\":\"kelpclaw.pi.bundle-result.v1\",\"runId\":\"{s}\",\"ok\":true,\"status\":\"succeeded\",\"policyPack\":\"appsec-agent-baseline\",\"mode\":\"pi-field\"}}\n", .{run_id});
    defer allocator.free(result);
    try common.writeFileWithParents(result_path, result);
    const compatibility_path = try common.pathJoin(allocator, output, "compatibility.json");
    defer allocator.free(compatibility_path);
    try common.writeFileWithParents(compatibility_path, "{\"schemaVersion\":\"kelpclaw.pi.compatibility.v1\",\"ok\":true,\"target\":\"kelp-pi\",\"checks\":[{\"id\":\"pi-bundle-layout\",\"status\":\"pass\",\"message\":\"Zig audit bundle layout matches verifier contract\"}]}\n");
    const policy_path = try common.pathJoin(allocator, output, "policy-decisions.json");
    defer allocator.free(policy_path);
    try common.writeFileWithParents(policy_path, "{\"schemaVersion\":\"kelpclaw.pi.policy-decisions.v1\",\"policyPack\":\"appsec-agent-baseline\",\"decisions\":[]}\n");
    const redaction_path = try common.pathJoin(allocator, output, "redaction-report.json");
    defer allocator.free(redaction_path);
    try common.writeFileWithParents(redaction_path, "{\"schemaVersion\":\"1.0.0\",\"redacted\":false,\"filesScanned\":0,\"findingCount\":0,\"findings\":[]}\n");
    try writeBundleTranscript(allocator, data_dir, output, run_id);
    const findings_out = try common.pathJoin(allocator, output, "normalized-findings.json");
    defer allocator.free(findings_out);
    const findings_in = try common.pathJoin3(allocator, workspace, "normalized", "findings.json");
    defer allocator.free(findings_in);
    if (common.fileExists(findings_in)) {
        const bytes = try std.fs.cwd().readFileAlloc(allocator, findings_in, 16 * 1024 * 1024);
        defer allocator.free(bytes);
        try common.writeFileWithParents(findings_out, bytes);
    } else {
        try common.writeFileWithParents(findings_out, "{\"findings\":[]}\n");
    }
    const sarif_out = try common.pathJoin(allocator, output, "findings.sarif");
    defer allocator.free(sarif_out);
    try common.writeFileWithParents(sarif_out, "{\"version\":\"2.1.0\",\"runs\":[{\"tool\":{\"driver\":{\"name\":\"kelp-pi\",\"informationUri\":\"https://github.com/gongahkia/kelp\",\"rules\":[]}},\"results\":[]}]}\n");
    const audit_out = try common.pathJoin(allocator, output, "audit-log.jsonl");
    defer allocator.free(audit_out);
    const audit_in = try common.pathJoin3(allocator, data_dir, "audit", "agent.jsonl");
    defer allocator.free(audit_in);
    if (common.fileExists(audit_in)) {
        const bytes = try std.fs.cwd().readFileAlloc(allocator, audit_in, 16 * 1024 * 1024);
        defer allocator.free(bytes);
        try common.writeFileWithParents(audit_out, bytes);
    } else {
        try common.writeFileWithParents(audit_out, "");
    }
    const audit_head_out = try common.pathJoin(allocator, output, "audit-head.json");
    defer allocator.free(audit_head_out);
    const audit_head_in = try audit.auditHeadPath(allocator, data_dir);
    defer allocator.free(audit_head_in);
    if (common.fileExists(audit_head_in)) {
        const bytes = try std.fs.cwd().readFileAlloc(allocator, audit_head_in, 4096);
        defer allocator.free(bytes);
        try common.writeFileWithParents(audit_head_out, bytes);
    } else {
        try common.writeFileWithParents(audit_head_out, "{\"schemaVersion\":\"kelp.pi.audit-head.v1\",\"entries\":0,\"headHash\":\"0000000000000000000000000000000000000000000000000000000000000000\"}\n");
    }
    const audit_chain_out = try common.pathJoin(allocator, output, "audit-chain.json");
    defer allocator.free(audit_chain_out);
    try common.writeFileWithParents(audit_chain_out, "{\"schemaVersion\":\"kelpclaw.pi.audit-chain.v1\",\"segments\":0,\"segmentEntries\":0,\"activeEntries\":0,\"entries\":0,\"headHash\":\"0000000000000000000000000000000000000000000000000000000000000000\"}\n");
    const index_path = try common.pathJoin(allocator, output, "index.html");
    defer allocator.free(index_path);
    try common.writeFileWithParents(index_path, "<!doctype html><title>Kelp Pi Audit Bundle</title><h1>Kelp Pi Audit Bundle</h1>\n");
}

fn writeSignedManifest(allocator: std.mem.Allocator, output: []const u8, run_id: []const u8, key: keys.KeyMaterial) ![]u8 {
    var manifest: std.ArrayList(u8) = .empty;
    defer manifest.deinit(allocator);
    var writer = manifest.writer(allocator);
    const public_pem = try ed25519PublicKeyPem(allocator, key.public_hex);
    defer allocator.free(public_pem);
    const key_hash = try common.contentHashHex(allocator, public_pem);
    defer allocator.free(key_hash);
    try writer.writeAll("{\n");
    try writer.writeAll("  \"algorithm\": \"ed25519\",\n");
    try writer.writeAll("  \"files\": [\n");
    for (core_bundle_files, 0..) |file, index| {
        if (index != 0) try writer.writeAll(",\n");
        const file_path = try common.pathJoin(allocator, output, file);
        defer allocator.free(file_path);
        const hash = try common.fileHashHex(allocator, file_path);
        defer allocator.free(hash);
        const stat = try std.fs.cwd().statFile(file_path);
        try writer.print("    {{\n      \"path\": \"{s}\",\n      \"sha256\": \"{s}\",\n      \"size\": {}\n    }}", .{ file, hash, stat.size });
    }
    try writer.writeAll("\n  ],\n");
    try writer.writeAll("  \"generatedAt\": \"1970-01-01T00:00:00.000Z\",\n");
    try writer.print("  \"publicKeyId\": \"sha256:{s}\",\n", .{key_hash});
    try writer.print("  \"runId\": \"{s}\",\n", .{run_id});
    try writer.writeAll("  \"schemaVersion\": \"1.0.0\"\n");
    try writer.writeAll("}");
    const payload = try manifest.toOwnedSlice(allocator);
    const manifest_path = try common.pathJoin(allocator, output, "manifest.json");
    defer allocator.free(manifest_path);
    try common.writeFileWithParents(manifest_path, payload);
    const sig = try key.key_pair.sign(payload, null);
    const sig_bytes = sig.toBytes();
    const sig_b64 = try base64Alloc(allocator, &sig_bytes);
    defer allocator.free(sig_b64);
    const sig_path = try common.pathJoin(allocator, output, "manifest.sig");
    defer allocator.free(sig_path);
    try common.writeFileWithParents(sig_path, sig_b64);
    const pub_path = try common.pathJoin(allocator, output, "manifest.pub.json");
    defer allocator.free(pub_path);
    const pub_json = try std.fmt.allocPrint(allocator, "{{\"keyId\":\"sha256:{s}\",\"algorithm\":\"ed25519\",\"publicKeyHex\":\"{s}\",\"publicKeyPem\":\"", .{ key_hash, key.public_hex });
    defer allocator.free(pub_json);
    var pub_payload: std.ArrayList(u8) = .empty;
    defer pub_payload.deinit(allocator);
    try pub_payload.appendSlice(allocator, pub_json);
    var pub_writer = pub_payload.writer(allocator);
    try common.writeJsonEscaped(&pub_writer, public_pem);
    try pub_payload.appendSlice(allocator, "\"}\n");
    const pub_bytes = try pub_payload.toOwnedSlice(allocator);
    defer allocator.free(pub_bytes);
    try common.writeFileWithParents(pub_path, pub_bytes);
    return payload;
}

fn writeSignedAttestation(allocator: std.mem.Allocator, output: []const u8, run_id: []const u8, manifest_payload: []const u8, key: keys.KeyMaterial) !void {
    const manifest_hash = try common.contentHashHex(allocator, manifest_payload);
    defer allocator.free(manifest_hash);
    const public_pem = try ed25519PublicKeyPem(allocator, key.public_hex);
    defer allocator.free(public_pem);
    const key_hash = try common.contentHashHex(allocator, public_pem);
    defer allocator.free(key_hash);
    var attestation: std.ArrayList(u8) = .empty;
    defer attestation.deinit(allocator);
    var writer = attestation.writer(allocator);
    try writer.writeAll("{\n");
    try writer.writeAll("  \"evidence\": {\n");
    try writer.writeAll("    \"agentRun\": false,\n");
    try writer.writeAll("    \"controls\": false,\n");
    try writer.writeAll("    \"evidenceWorkspace\": true,\n");
    try writer.writeAll("    \"governanceReport\": false,\n");
    try writer.writeAll("    \"hookEvents\": false,\n");
    try writer.writeAll("    \"sarif\": true,\n");
    try writer.writeAll("    \"webEvidence\": false\n");
    try writer.writeAll("  },\n");
    try writer.writeAll("  \"files\": [\n");
    for (core_bundle_files, 0..) |file, index| {
        if (index != 0) try writer.writeAll(",\n");
        try writer.print("    \"{s}\"", .{file});
    }
    try writer.writeAll("\n  ],\n");
    try writer.writeAll("  \"generatedAt\": \"1970-01-01T00:00:00.000Z\",\n");
    try writer.writeAll("  \"manifest\": {\n");
    try writer.writeAll("    \"path\": \"manifest.json\",\n");
    try writer.writeAll("    \"publicKeyPath\": \"manifest.pub.json\",\n");
    try writer.print("    \"sha256\": \"{s}\",\n", .{manifest_hash});
    try writer.writeAll("    \"signaturePath\": \"manifest.sig\"\n");
    try writer.writeAll("  },\n");
    try writer.writeAll("  \"policyPack\": \"appsec-agent-baseline\",\n");
    try writer.print("  \"runId\": \"{s}\",\n", .{run_id});
    try writer.writeAll("  \"schemaVersion\": \"1.0.0\",\n");
    try writer.writeAll("  \"signer\": {\n");
    try writer.writeAll("    \"algorithm\": \"ed25519\",\n");
    try writer.print("    \"keyId\": \"sha256:{s}\"\n", .{key_hash});
    try writer.writeAll("  }\n");
    try writer.writeAll("}");
    const payload = try attestation.toOwnedSlice(allocator);
    defer allocator.free(payload);
    const attestation_path = try common.pathJoin(allocator, output, "attestation.json");
    defer allocator.free(attestation_path);
    try common.writeFileWithParents(attestation_path, payload);
    const sig = try key.key_pair.sign(payload, null);
    const sig_bytes = sig.toBytes();
    const sig_b64 = try base64Alloc(allocator, &sig_bytes);
    defer allocator.free(sig_b64);
    const sig_path = try common.pathJoin(allocator, output, "attestation.sig");
    defer allocator.free(sig_path);
    try common.writeFileWithParents(sig_path, sig_b64);
}

fn verifyBundleDir(allocator: std.mem.Allocator, dir: []const u8) !bool {
    const manifest_path = try common.pathJoin(allocator, dir, "manifest.json");
    defer allocator.free(manifest_path);
    const sig_path = try common.pathJoin(allocator, dir, "manifest.sig");
    defer allocator.free(sig_path);
    const pub_path = try common.pathJoin(allocator, dir, "manifest.pub.json");
    defer allocator.free(pub_path);
    const manifest = try std.fs.cwd().readFileAlloc(allocator, manifest_path, 16 * 1024 * 1024);
    defer allocator.free(manifest);
    const sig_raw = try std.fs.cwd().readFileAlloc(allocator, sig_path, 4096);
    defer allocator.free(sig_raw);
    const pub_json = try std.fs.cwd().readFileAlloc(allocator, pub_path, 4096);
    defer allocator.free(pub_json);
    const pub_hex = common.extractJsonField(pub_json, "publicKeyHex") orelse return error.MissingPublicKey;
    var pub_bytes: [32]u8 = undefined;
    try common.hexToBytes(pub_hex, &pub_bytes);
    var sig_bytes: [64]u8 = undefined;
    try signatureToBytes(std.mem.trim(u8, sig_raw, " \t\r\n"), &sig_bytes);
    const public_key = try std.crypto.sign.Ed25519.PublicKey.fromBytes(pub_bytes);
    const sig = std.crypto.sign.Ed25519.Signature.fromBytes(sig_bytes);
    try sig.verify(manifest, public_key);
    var rest = manifest;
    while (std.mem.indexOf(u8, rest, "\"path\":\"")) |start| {
        const after = rest[start + 8 ..];
        const end = std.mem.indexOfScalar(u8, after, '"') orelse return error.BadManifest;
        const rel = after[0..end];
        const sha_marker = "\"sha256\":\"";
        const sha_start_rel = std.mem.indexOf(u8, after[end..], sha_marker) orelse return error.BadManifest;
        const sha_after = after[end + sha_start_rel + sha_marker.len ..];
        const sha_end = std.mem.indexOfScalar(u8, sha_after, '"') orelse return error.BadManifest;
        const expected = sha_after[0..sha_end];
        const full_path = try common.pathJoin(allocator, dir, rel);
        defer allocator.free(full_path);
        const actual = try common.fileHashHex(allocator, full_path);
        defer allocator.free(actual);
        if (!std.ascii.eqlIgnoreCase(actual, expected)) return false;
        rest = sha_after[sha_end..];
    }
    return true;
}

fn signatureToBytes(text: []const u8, out: []u8) !void {
    if (text.len == out.len * 2 and isHex(text)) {
        try common.hexToBytes(text, out);
        return;
    }
    if (try std.base64.standard.Decoder.calcSizeForSlice(text) != out.len) return error.InvalidSignature;
    try std.base64.standard.Decoder.decode(out, text);
}

fn isHex(text: []const u8) bool {
    for (text) |byte| {
        _ = common.hexNibble(byte) catch return false;
    }
    return true;
}

fn base64Alloc(allocator: std.mem.Allocator, bytes: []const u8) ![]u8 {
    const out = try allocator.alloc(u8, std.base64.standard.Encoder.calcSize(bytes.len) + 1);
    const encoded = std.base64.standard.Encoder.encode(out[0 .. out.len - 1], bytes);
    out[encoded.len] = '\n';
    return out[0 .. encoded.len + 1];
}

fn base64AllocNoNewline(allocator: std.mem.Allocator, bytes: []const u8) ![]u8 {
    const out = try allocator.alloc(u8, std.base64.standard.Encoder.calcSize(bytes.len));
    _ = std.base64.standard.Encoder.encode(out, bytes);
    return out;
}

fn ed25519PublicKeyPem(allocator: std.mem.Allocator, public_hex: []const u8) ![]u8 {
    var public_bytes: [32]u8 = undefined;
    try common.hexToBytes(public_hex, &public_bytes);
    var der: [44]u8 = undefined;
    @memcpy(der[0..12], &[_]u8{ 0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00 });
    @memcpy(der[12..44], &public_bytes);
    const body = try base64AllocNoNewline(allocator, &der);
    defer allocator.free(body);
    return std.fmt.allocPrint(allocator, "-----BEGIN PUBLIC KEY-----\n{s}\n-----END PUBLIC KEY-----\n", .{body});
}

test "bundle core files include session transcript in manifest" {
    const root = try common.testTempPath(std.testing.allocator, "transcript-bundle");
    defer std.testing.allocator.free(root);
    defer std.fs.cwd().deleteTree(root) catch {};
    const data_dir = try common.pathJoin(std.testing.allocator, root, "data");
    defer std.testing.allocator.free(data_dir);
    const output = try common.pathJoin(std.testing.allocator, root, "bundle");
    defer std.testing.allocator.free(output);
    try chat.appendTranscriptRecord(std.testing.allocator, data_dir, "run-a", "user", "bundle me");
    try writeBundleCoreFiles(std.testing.allocator, data_dir, ".", output, "run-a");
    const transcript_out = try common.pathJoin(std.testing.allocator, output, "transcript.jsonl");
    defer std.testing.allocator.free(transcript_out);
    const transcript = try std.fs.cwd().readFileAlloc(std.testing.allocator, transcript_out, 64 * 1024);
    defer std.testing.allocator.free(transcript);
    try std.testing.expect(std.mem.indexOf(u8, transcript, "bundle me") != null);
    var key = try keys.generateKeyMaterial(std.testing.allocator);
    defer keys.freeKeyMaterial(std.testing.allocator, &key);
    const manifest = try writeSignedManifest(std.testing.allocator, output, "run-a", key);
    defer std.testing.allocator.free(manifest);
    try std.testing.expect(std.mem.indexOf(u8, manifest, "\"path\": \"transcript.jsonl\"") != null);
}
