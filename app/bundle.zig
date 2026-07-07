const std = @import("std");
const audit = @import("audit.zig");
const common = @import("common.zig");
const chat = @import("chat.zig");
const keys = @import("keys.zig");

pub fn bundleCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "assemble")) return common.fail("usage: kelp-pi bundle assemble --run-id ID --workspace DIR --output DIR", 64);
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
    const policy_path = try common.pathJoin(allocator, output, "policy-decisions.json");
    defer allocator.free(policy_path);
    try common.writeFileWithParents(policy_path, "{\"schemaVersion\":\"kelpclaw.pi.policy-decisions.v1\",\"policyPack\":\"appsec-agent-baseline\",\"decisions\":[]}\n");
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
    const index_path = try common.pathJoin(allocator, output, "index.html");
    defer allocator.free(index_path);
    try common.writeFileWithParents(index_path, "<!doctype html><title>Kelp Pi Audit Bundle</title><h1>Kelp Pi Audit Bundle</h1>\n");
}

fn writeSignedManifest(allocator: std.mem.Allocator, output: []const u8, run_id: []const u8, key: keys.KeyMaterial) ![]u8 {
    const files = [_][]const u8{ "result.json", "policy-decisions.json", "transcript.jsonl", "normalized-findings.json", "audit-log.jsonl", "audit-head.json", "index.html" };
    var manifest: std.ArrayList(u8) = .empty;
    defer manifest.deinit(allocator);
    var writer = manifest.writer(allocator);
    try writer.print("{{\"schemaVersion\":\"1.0.0\",\"runId\":\"{s}\",\"algorithm\":\"ed25519\",\"publicKeyId\":\"{s}\",\"files\":[", .{ run_id, key.public_hex[0..16] });
    for (files, 0..) |file, index| {
        if (index != 0) try writer.writeAll(",");
        const file_path = try common.pathJoin(allocator, output, file);
        defer allocator.free(file_path);
        const hash = try common.fileHashHex(allocator, file_path);
        defer allocator.free(hash);
        const stat = try std.fs.cwd().statFile(file_path);
        try writer.print("{{\"path\":\"{s}\",\"size\":{},\"sha256\":\"{s}\"}}", .{ file, stat.size, hash });
    }
    try writer.writeAll("]}\n");
    const payload = try manifest.toOwnedSlice(allocator);
    const manifest_path = try common.pathJoin(allocator, output, "manifest.json");
    defer allocator.free(manifest_path);
    try common.writeFileWithParents(manifest_path, payload);
    const sig = try key.key_pair.sign(payload, null);
    const sig_bytes = sig.toBytes();
    const sig_hex = try common.hexAlloc(allocator, &sig_bytes);
    defer allocator.free(sig_hex);
    const sig_path = try common.pathJoin(allocator, output, "manifest.sig");
    defer allocator.free(sig_path);
    try common.writeFileWithParents(sig_path, sig_hex);
    const pub_path = try common.pathJoin(allocator, output, "manifest.pub.json");
    defer allocator.free(pub_path);
    const pub_json = try std.fmt.allocPrint(allocator, "{{\"keyId\":\"{s}\",\"algorithm\":\"ed25519\",\"publicKeyHex\":\"{s}\"}}\n", .{ key.public_hex[0..16], key.public_hex });
    defer allocator.free(pub_json);
    try common.writeFileWithParents(pub_path, pub_json);
    return payload;
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
    const sig_hex_raw = try std.fs.cwd().readFileAlloc(allocator, sig_path, 4096);
    defer allocator.free(sig_hex_raw);
    const pub_json = try std.fs.cwd().readFileAlloc(allocator, pub_path, 4096);
    defer allocator.free(pub_json);
    const pub_hex = common.extractJsonField(pub_json, "publicKeyHex") orelse return error.MissingPublicKey;
    var pub_bytes: [32]u8 = undefined;
    try common.hexToBytes(pub_hex, &pub_bytes);
    var sig_bytes: [64]u8 = undefined;
    try common.hexToBytes(std.mem.trim(u8, sig_hex_raw, " \t\r\n"), &sig_bytes);
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
    try std.testing.expect(std.mem.indexOf(u8, manifest, "\"path\":\"transcript.jsonl\"") != null);
}
