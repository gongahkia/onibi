const std = @import("std");
const build_options = @import("build_options");
const sqlite = @cImport({
    @cInclude("sqlite3.h");
});

const LlamaResult = extern struct {
    ok: c_int,
    loaded: c_int,
    decoded_tokens: c_int,
    elapsed_seconds: f64,
    peak_rss_bytes: i64,
    text: [2048]u8,
    @"error": [256]u8,
};

extern fn kelp_llama_generate(model_path: [*:0]const u8, prompt: [*:0]const u8, n_predict: c_int, n_threads: c_int, result: *LlamaResult) c_int;

const default_data_dir = "/var/lib/kelp-pi";
const default_policy_path = "policies/appsec-agent-baseline.toml";
const default_model_manifest = "models/manifest.toml";
const default_model_id = "qwen3-0.6b-q4_k_m";
const default_model_smoke_prompt =
    \\<|im_start|>user
    \\/no_think Reply with exactly: ready<|im_end|>
    \\<|im_start|>assistant
;
const default_nuclei_bin = "/opt/kelp-pi/bin/nuclei";
const pinned_nuclei_templates_revision = "cce82b61d26bed35074cd57bc9d0aebd703a81d3";
const scanner_users_set = "scanner_users";
const scanner_targets_set = "scanner_ipv4_targets";
const default_scanner_user = "kelp-pi-scanner";
const default_systemd_run = "systemd-run";
const default_nft_bin = "nft";
const default_no_answer_threshold = 0.000001;

const Action = enum {
    allow,
    log_only,
    require_approval,
    deny,

    fn rank(self: Action) u8 {
        return switch (self) {
            .allow => 0,
            .log_only => 1,
            .require_approval => 2,
            .deny => 3,
        };
    }

    fn text(self: Action) []const u8 {
        return switch (self) {
            .allow => "allow",
            .log_only => "log-only",
            .require_approval => "require-approval",
            .deny => "deny",
        };
    }
};

const StringList = struct {
    items: [64][]const u8 = undefined,
    len: usize = 0,

    fn push(self: *StringList, value: []const u8) void {
        if (self.len < self.items.len) {
            self.items[self.len] = value;
            self.len += 1;
        }
    }

    fn anyIn(self: StringList, haystack: []const u8) bool {
        var index: usize = 0;
        while (index < self.len) : (index += 1) {
            if (containsIgnoreCase(haystack, self.items[index])) return true;
        }
        return false;
    }
};

const Rule = struct {
    id: []const u8 = "",
    tool: []const u8 = "",
    command_any: StringList = .{},
    command_any_secondary: StringList = .{},
    action: Action = .allow,
    approver_role: []const u8 = "",

    fn matches(self: Rule, tool: []const u8, command: []const u8) bool {
        if (self.tool.len != 0 and !std.mem.eql(u8, self.tool, tool)) return false;
        if (self.command_any.len != 0 and !self.command_any.anyIn(command)) return false;
        if (self.command_any_secondary.len != 0 and !self.command_any_secondary.anyIn(command)) return false;
        return true;
    }
};

const RuleSet = struct {
    rules: [64]Rule = undefined,
    len: usize = 0,

    fn push(self: *RuleSet, rule: Rule) void {
        if (self.len < self.rules.len) {
            self.rules[self.len] = rule;
            self.len += 1;
        }
    }
};

const Decision = struct {
    action: Action,
    selected_rule: []const u8,
    matched: StringList,
    approver_role: []const u8,
};

const ModelEntry = struct {
    id: []const u8 = "",
    url: []const u8 = "",
    sha256: []const u8 = "",
    ram_floor_mb: u64 = 0,
    primary: bool = false,
};

const ModelManifest = struct {
    models: [16]ModelEntry = undefined,
    len: usize = 0,

    fn push(self: *ModelManifest, model: ModelEntry) void {
        if (model.id.len == 0 or self.len >= self.models.len) return;
        self.models[self.len] = model;
        self.len += 1;
    }

    fn find(self: ModelManifest, id: []const u8) ?ModelEntry {
        var index: usize = 0;
        while (index < self.len) : (index += 1) {
            if (std.mem.eql(u8, self.models[index].id, id)) return self.models[index];
        }
        return null;
    }
};

const KeyMaterial = struct {
    key_pair: std.crypto.sign.Ed25519.KeyPair,
    public_hex: []u8,
    private_hex: []u8,
};

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();
    const args = try std.process.argsAlloc(allocator);
    defer std.process.argsFree(allocator, args);

    if (args.len <= 1) return usage();
    const command = args[1];
    if (std.mem.eql(u8, command, "version") or std.mem.eql(u8, command, "--version")) return printLine("kelp-pi 0.2.0-zig");
    if (std.mem.eql(u8, command, "doctor")) return doctor(args[2..]);
    if (std.mem.eql(u8, command, "keygen")) return keygen(allocator, args[2..]);
    if (std.mem.eql(u8, command, "policy")) return policyCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "approval-request")) return approvalRequest(allocator, args[2..]);
    if (std.mem.eql(u8, command, "approve")) return approve(allocator, args[2..]);
    if (std.mem.eql(u8, command, "scope")) return scopeCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "scan")) return scanCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "index")) return indexCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "ask")) return askCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "bundle")) return bundleCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "verify-bundle")) return verifyBundle(allocator, args[2..]);
    if (std.mem.eql(u8, command, "model")) return modelCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "chat")) return chatCommand(args[2..]);
    return fail("unknown command", 64);
}

fn usage() !void {
    return printLine("usage: kelp-pi <chat|doctor|keygen|policy|approval-request|approve|scope|scan|index|ask|bundle|verify-bundle|model|version>");
}

fn doctor(args: []const []const u8) !void {
    const data_dir = option(args, "--data-dir") orelse default_data_dir;
    const policy_path = option(args, "--policy") orelse default_policy_path;
    const model_manifest = option(args, "--models") orelse default_model_manifest;
    const policy_ok = fileExists(policy_path);
    const model_ok = fileExists(model_manifest);
    const sqlite_ok = sqlite.sqlite3_libversion_number() > 0;
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print(
        "{{\"ok\":{},\"dataDir\":\"{s}\",\"checks\":[{{\"id\":\"policy-pack\",\"status\":\"{s}\"}},{{\"id\":\"model-manifest\",\"status\":\"{s}\"}},{{\"id\":\"sqlite3\",\"status\":\"{s}\"}},{{\"id\":\"llama-linked\",\"status\":\"{s}\"}}]}}\n",
        .{ policy_ok and model_ok and sqlite_ok, data_dir, if (policy_ok) "pass" else "fail", if (model_ok) "pass" else "fail", if (sqlite_ok) "pass" else "fail", if (build_options.have_llama) "pass" else "warn" },
    );
}

fn keygen(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const data_dir = option(args, "--data-dir") orelse default_data_dir;
    const label = option(args, "--label") orelse "kelp-pi";
    var key = try generateKeyMaterial(allocator);
    defer freeKeyMaterial(allocator, &key);
    const key_dir = try pathJoin(allocator, data_dir, "keys");
    defer allocator.free(key_dir);
    try std.fs.cwd().makePath(key_dir);
    const key_path = try pathJoin(allocator, key_dir, "pi-ed25519.key.json");
    defer allocator.free(key_path);
    const payload = try std.fmt.allocPrint(
        allocator,
        "{{\"schemaVersion\":\"kelp.pi.key.v1\",\"algorithm\":\"ed25519\",\"label\":\"{s}\",\"publicKeyHex\":\"{s}\",\"privateKeyHex\":\"{s}\"}}\n",
        .{ label, key.public_hex, key.private_hex },
    );
    defer allocator.free(payload);
    try writeFileWithParents(key_path, payload);
    chmod600(key_path);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"keyPath\":\"{s}\",\"label\":\"{s}\",\"publicKeyHex\":\"{s}\"}}\n", .{ key_path, label, key.public_hex });
}

fn policyCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "check")) return fail("usage: kelp-pi policy check --tool TOOL --command CMD", 64);
    const tool = option(args[1..], "--tool") orelse "Bash";
    const command = option(args[1..], "--command") orelse "";
    const policy_path = option(args[1..], "--policy") orelse default_policy_path;
    const policy_text = try std.fs.cwd().readFileAlloc(allocator, policy_path, 1024 * 1024);
    defer allocator.free(policy_text);
    return printDecision(evaluatePolicy(parseRules(policy_text), tool, command));
}

fn approvalRequest(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const data_dir = option(args, "--data-dir") orelse default_data_dir;
    const scope_id = option(args, "--scope-id") orelse "default";
    const command = option(args, "--command") orelse "";
    const ttl_raw = option(args, "--ttl-seconds") orelse "300";
    const ttl = std.fmt.parseInt(i64, ttl_raw, 10) catch 300;
    var token_bytes: [16]u8 = undefined;
    std.crypto.random.bytes(&token_bytes);
    const token = try hexAlloc(allocator, &token_bytes);
    defer allocator.free(token);
    const approvals_dir = try pathJoin(allocator, data_dir, "approvals");
    defer allocator.free(approvals_dir);
    try std.fs.cwd().makePath(approvals_dir);
    const record_path_name = try std.fmt.allocPrint(allocator, "{s}.json", .{token});
    defer allocator.free(record_path_name);
    const record_path = try pathJoin(allocator, approvals_dir, record_path_name);
    defer allocator.free(record_path);
    const now = std.time.timestamp();
    const payload = try std.fmt.allocPrint(
        allocator,
        "{{\"token\":\"{s}\",\"scopeId\":\"{s}\",\"command\":\"{s}\",\"status\":\"pending\",\"createdAtUnix\":{},\"expiresAtUnix\":{}}}\n",
        .{ token, scope_id, command, now, now + ttl },
    );
    defer allocator.free(payload);
    try writeFileWithParents(record_path, payload);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"token\":\"{s}\",\"path\":\"{s}\",\"status\":\"pending\",\"expiresAtUnix\":{}}}\n", .{ token, record_path, now + ttl });
}

fn approve(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const data_dir = option(args, "--data-dir") orelse default_data_dir;
    const token = firstPositional(args, "--data-dir") orelse return fail("usage: kelp-pi approve TOKEN [--data-dir DIR]", 64);
    const record_path = try approvalPath(allocator, data_dir, token);
    defer allocator.free(record_path);
    const existing = std.fs.cwd().readFileAlloc(allocator, record_path, 1024 * 1024) catch return fail("approval token not found", 77);
    defer allocator.free(existing);
    const updated = try replaceStatusApproved(allocator, existing);
    defer allocator.free(updated);
    try writeFileWithParents(record_path, updated);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"token\":\"{s}\",\"status\":\"approved\"}}\n", .{token});
}

fn scopeCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "set")) return fail("usage: kelp-pi scope set --host HOST --until RFC3339", 64);
    const data_dir = option(args[1..], "--data-dir") orelse default_data_dir;
    const host = option(args[1..], "--host") orelse option(args[1..], "--url") orelse return fail("scope requires --host or --url", 64);
    const until = option(args[1..], "--until") orelse "manual";
    const scope_id = option(args[1..], "--scope-id") orelse "default";
    const scope_dir = try pathJoin(allocator, data_dir, "scope");
    defer allocator.free(scope_dir);
    try std.fs.cwd().makePath(scope_dir);
    const scope_path = try pathJoin(allocator, scope_dir, "current-scope.json");
    defer allocator.free(scope_path);
    const payload = try std.fmt.allocPrint(
        allocator,
        "{{\"schemaVersion\":\"kelp.pi.scope.v1\",\"scopeId\":\"{s}\",\"host\":\"{s}\",\"validUntil\":\"{s}\"}}\n",
        .{ scope_id, host, until },
    );
    defer allocator.free(payload);
    try writeFileWithParents(scope_path, payload);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"scopeId\":\"{s}\",\"host\":\"{s}\",\"path\":\"{s}\"}}\n", .{ scope_id, host, scope_path });
}

fn scanCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0) return fail("usage: kelp-pi scan <nuclei|nmap|zap> --target TARGET", 64);
    const scanner = args[0];
    const target = option(args[1..], "--target") orelse return fail("scan requires --target", 64);
    const data_dir = option(args[1..], "--data-dir") orelse default_data_dir;
    const token = option(args[1..], "--approval-token");
    const scanner_bin = option(args[1..], "--scanner-bin") orelse defaultScannerBin(scanner);
    const systemd_run_bin = option(args[1..], "--systemd-run-bin") orelse default_systemd_run;
    const nft_bin = option(args[1..], "--nft-bin") orelse default_nft_bin;
    const sandbox = hasFlag(args[1..], "--sandbox");
    const dry_run = hasFlag(args[1..], "--dry-run");

    const command = try std.fmt.allocPrint(allocator, "{s} {s}", .{ scanner, target });
    defer allocator.free(command);
    const policy_text = try std.fs.cwd().readFileAlloc(allocator, default_policy_path, 1024 * 1024);
    defer allocator.free(policy_text);
    const decision = evaluatePolicy(parseRules(policy_text), "Bash", command);
    if (decision.action == .deny) return printDecision(decision);
    if (!targetInScope(allocator, data_dir, target)) return printJsonStatus(false, "deny", "target outside active scope");
    if (decision.action == .require_approval and (token == null or !approvalApproved(allocator, data_dir, token.?))) {
        return printJsonStatus(false, "require-approval", "approved token required");
    }

    var enforced: std.ArrayList([]const u8) = .empty;
    defer enforced.deinit(allocator);
    try appendScannerLimits(allocator, &enforced, scanner, args[1..]);
    const passthrough = argsAfterDoubleDash(args[1..]);
    const command_argv = try buildScannerArgv(allocator, scanner_bin, scanner, target, passthrough, enforced.items, sandbox, systemd_run_bin);
    defer freeArgv(allocator, command_argv);

    if (dry_run) {
        return printScanDryRun(scanner, scanner_bin, target, sandbox, command_argv);
    }
    if (sandbox) {
        if (option(args[1..], "--scanner-target-ip")) |ip| try reloadScannerTargetSet(allocator, nft_bin, ip);
    }
    const result = try std.process.Child.run(.{ .allocator = allocator, .argv = command_argv, .max_output_bytes = 10 * 1024 * 1024 });
    defer allocator.free(result.stdout);
    defer allocator.free(result.stderr);
    const success = switch (result.term) {
        .Exited => |code| code == 0,
        else => false,
    };
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":{},\"scanner\":\"{s}\",\"target\":\"{s}\",\"sandboxed\":{},\"stdoutBytes\":{},\"stderrBytes\":{}}}\n", .{ success, scanner, target, sandbox, result.stdout.len, result.stderr.len });
}

fn indexCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "ingest")) return fail("usage: kelp-pi index ingest --input PATH [--path PATH]", 64);
    const input = option(args[1..], "--input") orelse return fail("index ingest requires --input", 64);
    const logical = option(args[1..], "--path") orelse input;
    const data_dir = option(args[1..], "--data-dir") orelse default_data_dir;
    const content = try std.fs.cwd().readFileAlloc(allocator, input, 16 * 1024 * 1024);
    defer allocator.free(content);
    if (binaryIngestRefused(content)) return fail("binary ingest refused", 77);
    const index_dir = try pathJoin(allocator, data_dir, "index");
    defer allocator.free(index_dir);
    try std.fs.cwd().makePath(index_dir);
    const db_path = try pathJoin(allocator, index_dir, "chunks.sqlite3");
    defer allocator.free(db_path);
    const chunk_hash = try contentHashHex(allocator, content);
    defer allocator.free(chunk_hash);
    const chunk_id = try std.fmt.allocPrint(allocator, "{s}:{s}", .{ logical, chunk_hash[0..16] });
    defer allocator.free(chunk_id);
    const db = try sqliteOpen(db_path);
    defer _ = sqlite.sqlite3_close(db);
    try applyIndexSchema(db);
    const outcome = try ingestChunk(allocator, db, logical, chunk_id, chunk_hash, content);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"path\":\"{s}\",\"chunkId\":\"{s}\",\"outcome\":\"{s}\",\"db\":\"{s}\"}}\n", .{ logical, chunk_id, outcome, db_path });
}

fn askCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0) return fail("usage: kelp-pi ask QUERY [--data-dir DIR] [--top-k N]", 64);
    const query = args[0];
    const data_dir = option(args[1..], "--data-dir") orelse default_data_dir;
    const top_k = parseUsize(option(args[1..], "--top-k") orelse "3", 3);
    const db_path = try pathJoin3(allocator, data_dir, "index", "chunks.sqlite3");
    defer allocator.free(db_path);
    if (!fileExists(db_path)) {
        var out = std.fs.File.stdout().deprecatedWriter();
        return out.print("{{\"query\":\"{s}\",\"topK\":{},\"noAnswer\":{{\"reason\":\"no index\",\"threshold\":{},\"maxScore\":null}},\"citations\":[],\"results\":[]}}\n", .{ query, top_k, default_no_answer_threshold });
    }
    const db = try sqliteOpen(db_path);
    defer _ = sqlite.sqlite3_close(db);
    try applyIndexSchema(db);
    try searchChunks(allocator, db, query, top_k);
}

fn bundleCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "assemble")) return fail("usage: kelp-pi bundle assemble --run-id ID --workspace DIR --output DIR", 64);
    const run_id = option(args[1..], "--run-id") orelse "local";
    const workspace = option(args[1..], "--workspace") orelse ".";
    const output = option(args[1..], "--output") orelse "audit-bundle";
    const data_dir = option(args[1..], "--data-dir") orelse default_data_dir;
    const key_dir = try pathJoin(allocator, data_dir, "keys");
    defer allocator.free(key_dir);
    try std.fs.cwd().makePath(output);
    var key = try loadOrCreateKey(allocator, key_dir);
    defer freeKeyMaterial(allocator, &key);
    try writeBundleCoreFiles(allocator, data_dir, workspace, output, run_id);
    const manifest_payload = try writeSignedManifest(allocator, output, run_id, key);
    defer allocator.free(manifest_payload);
    const manifest_hash = try contentHashHex(allocator, manifest_payload);
    defer allocator.free(manifest_hash);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"runId\":\"{s}\",\"bundleDir\":\"{s}\",\"manifestSha256\":\"{s}\"}}\n", .{ run_id, output, manifest_hash });
}

fn verifyBundle(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0) return fail("usage: kelp-pi verify-bundle DIR", 64);
    const dir = args[0];
    const result = verifyBundleDir(allocator, dir) catch |err| {
        var out = std.fs.File.stdout().deprecatedWriter();
        return out.print("{{\"ok\":false,\"bundleDir\":\"{s}\",\"reason\":\"{s}\"}}\n", .{ dir, @errorName(err) });
    };
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":{},\"bundleDir\":\"{s}\",\"filesChecked\":{}}}\n", .{ result, dir, if (result) @as(usize, 1) else @as(usize, 0) });
}

fn modelCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0) return fail("usage: kelp-pi model <fetch|warm|verify|prompt> --id MODEL_ID", 64);
    const sub = args[0];
    const id = option(args[1..], "--id") orelse default_model_id;
    const manifest_path = option(args[1..], "--manifest") orelse default_model_manifest;
    const manifest_text = try std.fs.cwd().readFileAlloc(allocator, manifest_path, 1024 * 1024);
    defer allocator.free(manifest_text);
    const manifest = parseModelManifest(manifest_text);
    const model = manifest.find(id) orelse return printJsonStatus(false, "missing-model", "model id not in manifest");
    if (model.sha256.len == 0) return printJsonStatus(false, "missing-sha256", "model manifest sha256 is blank");
    const data_dir = option(args[1..], "--data-dir") orelse ".kelp-pi";
    const default_path = try modelCachePath(allocator, data_dir, model.url);
    defer allocator.free(default_path);
    const model_path = option(args[1..], "--model-path") orelse default_path;
    if (std.mem.eql(u8, sub, "fetch")) return modelFetch(allocator, model, model_path);
    if (std.mem.eql(u8, sub, "verify")) return modelVerify(allocator, model, model_path, false, null, 0, 0);
    if (std.mem.eql(u8, sub, "warm")) return modelVerify(allocator, model, model_path, true, option(args[1..], "--prompt") orelse default_model_smoke_prompt, parseUsize(option(args[1..], "--n-predict") orelse "1", 1), parseUsize(option(args[1..], "--threads") orelse "2", 2));
    if (std.mem.eql(u8, sub, "prompt")) return modelVerify(allocator, model, model_path, true, option(args[1..], "--prompt") orelse default_model_smoke_prompt, parseUsize(option(args[1..], "--n-predict") orelse "32", 32), parseUsize(option(args[1..], "--threads") orelse "2", 2));
    return fail("usage: kelp-pi model <fetch|warm|verify|prompt> --id MODEL_ID", 64);
}

fn chatCommand(args: []const []const u8) !void {
    const data_dir = option(args, "--data-dir") orelse default_data_dir;
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("kelp-pi chat session=local data-dir={s}\n", .{data_dir});
    try out.print("> ", .{});
    var stdin = std.fs.File.stdin().deprecatedReader();
    var buffer: [4096]u8 = undefined;
    while (try stdin.readUntilDelimiterOrEof(&buffer, '\n')) |line_raw| {
        const line = std.mem.trim(u8, line_raw, " \t\r\n");
        if (line.len == 0) {
            try out.print("> ", .{});
            continue;
        }
        if (std.mem.eql(u8, line, "exit") or std.mem.indexOf(u8, line, "<finish") != null) {
            try out.print("{{\"ok\":true,\"finished\":true}}\n", .{});
            break;
        }
        if (attr(line, "query")) |query| {
            try out.print("{{\"tool\":\"ask\",\"query\":\"{s}\"}}\n", .{query});
        } else if (attr(line, "target")) |target| {
            try out.print("{{\"tool\":\"scan\",\"target\":\"{s}\",\"policy\":\"require-approval\"}}\n", .{target});
        } else {
            try out.print("{{\"ok\":true,\"echo\":\"", .{});
            try writeJsonEscaped(&out, line);
            try out.print("\"}}\n", .{});
        }
        try out.print("> ", .{});
    }
}

fn modelFetch(allocator: std.mem.Allocator, model: ModelEntry, model_path: []const u8) !void {
    if (fileExists(model_path)) {
        return modelVerify(allocator, model, model_path, false, null, 0, 0);
    }
    if (std.fs.path.dirname(model_path)) |parent| try std.fs.cwd().makePath(parent);
    const result = try std.process.Child.run(.{ .allocator = allocator, .argv = &.{ "curl", "-fL", "--retry", "3", "-o", model_path, model.url }, .max_output_bytes = 1024 * 1024 });
    defer allocator.free(result.stdout);
    defer allocator.free(result.stderr);
    const ok = switch (result.term) {
        .Exited => |code| code == 0,
        else => false,
    };
    if (!ok) return fail("model download failed", 77);
    return modelVerify(allocator, model, model_path, false, null, 0, 0);
}

fn modelVerify(allocator: std.mem.Allocator, model: ModelEntry, model_path: []const u8, warm: bool, prompt: ?[]const u8, n_predict: usize, n_threads: usize) !void {
    if (!fileExists(model_path)) return printJsonStatus(false, "missing-model-file", "model path does not exist");
    const actual = try fileHashHex(allocator, model_path);
    defer allocator.free(actual);
    if (!std.ascii.eqlIgnoreCase(actual, model.sha256)) return printJsonStatus(false, "sha256-mismatch", "model file hash does not match manifest");
    if (!try fileHasMagic(model_path, "GGUF")) return printJsonStatus(false, "invalid-gguf", "model file missing GGUF magic");
    if (!ramGateAllows(model.ram_floor_mb)) return printJsonStatus(false, "ram-gate", "detected RAM below model floor");
    var out = std.fs.File.stdout().deprecatedWriter();
    if (warm and build_options.have_llama) {
        return modelGenerate(allocator, model, model_path, actual, prompt orelse "ready", n_predict, n_threads);
    } else {
        try out.print("{{\"ok\":true,\"id\":\"{s}\",\"path\":\"{s}\",\"sha256\":\"{s}\",\"runtime\":\"llama.cpp\",\"loaded\":false,\"reason\":\"libllama not linked in this build\"}}\n", .{ model.id, model_path, actual });
    }
}

fn modelGenerate(allocator: std.mem.Allocator, model: ModelEntry, model_path: []const u8, actual_hash: []const u8, prompt: []const u8, n_predict_raw: usize, n_threads_raw: usize) !void {
    const model_path_z = try allocator.dupeZ(u8, model_path);
    defer allocator.free(model_path_z);
    const prompt_z = try allocator.dupeZ(u8, prompt);
    defer allocator.free(prompt_z);
    var result: LlamaResult = undefined;
    const n_predict = @min(n_predict_raw, 512);
    const n_threads = @max(n_threads_raw, 1);
    const rc = kelp_llama_generate(model_path_z.ptr, prompt_z.ptr, @intCast(n_predict), @intCast(n_threads), &result);
    var out = std.fs.File.stdout().deprecatedWriter();
    if (rc != 0 or result.ok == 0) {
        try out.print("{{\"ok\":false,\"id\":\"{s}\",\"path\":\"{s}\",\"runtime\":\"llama.cpp\",\"loaded\":{},\"peakRssBytes\":{},\"reason\":\"", .{ model.id, model_path, result.loaded != 0, result.peak_rss_bytes });
        try writeJsonEscaped(&out, std.mem.sliceTo(result.@"error"[0..], 0));
        try out.print("\"}}\n", .{});
        return;
    }
    try out.print("{{\"ok\":true,\"id\":\"{s}\",\"path\":\"{s}\",\"sha256\":\"{s}\",\"runtime\":\"llama.cpp\",\"loaded\":true,\"decodedTokens\":{},\"elapsedSeconds\":{d:.3},\"peakRssBytes\":{},\"text\":\"", .{ model.id, model_path, actual_hash, result.decoded_tokens, result.elapsed_seconds, result.peak_rss_bytes });
    try writeJsonEscaped(&out, std.mem.sliceTo(result.text[0..], 0));
    try out.print("\"}}\n", .{});
}

fn sqliteOpen(path: []const u8) !*sqlite.sqlite3 {
    var db: ?*sqlite.sqlite3 = null;
    var path_buf: [4096:0]u8 = undefined;
    const zpath = try std.fmt.bufPrintZ(&path_buf, "{s}", .{path});
    if (sqlite.sqlite3_open(zpath.ptr, &db) != sqlite.SQLITE_OK) return error.SqliteOpen;
    return db.?;
}

fn sqliteExec(db: *sqlite.sqlite3, sql_text: []const u8) !void {
    var sql_buf: [4096:0]u8 = undefined;
    const zsql = try std.fmt.bufPrintZ(&sql_buf, "{s}", .{sql_text});
    if (sqlite.sqlite3_exec(db, zsql.ptr, null, null, null) != sqlite.SQLITE_OK) return error.SqliteExec;
}

fn applyIndexSchema(db: *sqlite.sqlite3) !void {
    try sqliteExec(db,
        \\CREATE TABLE IF NOT EXISTS chunks (
        \\ id TEXT PRIMARY KEY,
        \\ path TEXT NOT NULL,
        \\ heading_path TEXT NOT NULL,
        \\ start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
        \\ end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
        \\ content_hash TEXT NOT NULL,
        \\ content TEXT NOT NULL,
        \\ ingested_at TEXT NOT NULL
        \\);
        \\CREATE TABLE IF NOT EXISTS source_files (
        \\ path TEXT PRIMARY KEY,
        \\ content_hash TEXT NOT NULL,
        \\ mtime_unix_nanos INTEGER NOT NULL CHECK (mtime_unix_nanos >= 0),
        \\ size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
        \\ chunk_count INTEGER NOT NULL CHECK (chunk_count >= 0),
        \\ ingested_at TEXT NOT NULL
        \\);
        \\CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(content, content='chunks', content_rowid='rowid');
    );
}

fn ingestChunk(allocator: std.mem.Allocator, db: *sqlite.sqlite3, path: []const u8, chunk_id: []const u8, hash: []const u8, content: []const u8) ![]const u8 {
    const existing = try sourceHash(allocator, db, path);
    if (existing) |existing_hash| {
        defer allocator.free(existing_hash);
        if (std.mem.eql(u8, existing_hash, hash)) return "unchanged";
    }
    try sqliteExec(db, "BEGIN IMMEDIATE");
    try execDeletePath(db, "DELETE FROM chunks WHERE path = ?1", path);
    try execInsertChunk(db, path, chunk_id, hash, content);
    try execUpsertSource(db, path, hash, content.len);
    try sqliteExec(db, "INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')");
    try sqliteExec(db, "COMMIT");
    return "replaced";
}

fn binaryIngestRefused(content: []const u8) bool {
    return std.mem.indexOfScalar(u8, content, 0) != null;
}

fn prepare(db: *sqlite.sqlite3, sql_text: []const u8) !*sqlite.sqlite3_stmt {
    var stmt: ?*sqlite.sqlite3_stmt = null;
    var sql_buf: [2048:0]u8 = undefined;
    const zsql = try std.fmt.bufPrintZ(&sql_buf, "{s}", .{sql_text});
    if (sqlite.sqlite3_prepare_v2(db, zsql.ptr, -1, &stmt, null) != sqlite.SQLITE_OK) return error.SqlitePrepare;
    return stmt.?;
}

fn bindText(stmt: *sqlite.sqlite3_stmt, index: c_int, value: []const u8) !void {
    if (sqlite.sqlite3_bind_text(stmt, index, value.ptr, @intCast(value.len), null) != sqlite.SQLITE_OK) return error.SqliteBind;
}

fn sourceHash(allocator: std.mem.Allocator, db: *sqlite.sqlite3, path: []const u8) !?[]u8 {
    const stmt = try prepare(db, "SELECT content_hash FROM source_files WHERE path = ?1");
    defer _ = sqlite.sqlite3_finalize(stmt);
    try bindText(stmt, 1, path);
    const rc = sqlite.sqlite3_step(stmt);
    if (rc == sqlite.SQLITE_ROW) {
        const text = sqlite.sqlite3_column_text(stmt, 0);
        if (text == null) return null;
        const len = sqlite.sqlite3_column_bytes(stmt, 0);
        return try allocator.dupe(u8, text[0..@intCast(len)]);
    }
    if (rc == sqlite.SQLITE_DONE) return null;
    return error.SqliteStep;
}

fn execDeletePath(db: *sqlite.sqlite3, sql_text: []const u8, path: []const u8) !void {
    const stmt = try prepare(db, sql_text);
    defer _ = sqlite.sqlite3_finalize(stmt);
    try bindText(stmt, 1, path);
    if (sqlite.sqlite3_step(stmt) != sqlite.SQLITE_DONE) return error.SqliteStep;
}

fn execInsertChunk(db: *sqlite.sqlite3, path: []const u8, chunk_id: []const u8, hash: []const u8, content: []const u8) !void {
    const stmt = try prepare(db, "INSERT INTO chunks (id, path, heading_path, start_byte, end_byte, content_hash, content, ingested_at) VALUES (?1, ?2, '[]', 0, ?3, ?4, ?5, datetime('now'))");
    defer _ = sqlite.sqlite3_finalize(stmt);
    try bindText(stmt, 1, chunk_id);
    try bindText(stmt, 2, path);
    if (sqlite.sqlite3_bind_int64(stmt, 3, @intCast(content.len)) != sqlite.SQLITE_OK) return error.SqliteBind;
    try bindText(stmt, 4, hash);
    try bindText(stmt, 5, content);
    if (sqlite.sqlite3_step(stmt) != sqlite.SQLITE_DONE) return error.SqliteStep;
}

fn execUpsertSource(db: *sqlite.sqlite3, path: []const u8, hash: []const u8, size: usize) !void {
    const stmt = try prepare(db, "INSERT INTO source_files (path, content_hash, mtime_unix_nanos, size_bytes, chunk_count, ingested_at) VALUES (?1, ?2, 0, ?3, 1, datetime('now')) ON CONFLICT(path) DO UPDATE SET content_hash = excluded.content_hash, size_bytes = excluded.size_bytes, chunk_count = excluded.chunk_count, ingested_at = excluded.ingested_at");
    defer _ = sqlite.sqlite3_finalize(stmt);
    try bindText(stmt, 1, path);
    try bindText(stmt, 2, hash);
    if (sqlite.sqlite3_bind_int64(stmt, 3, @intCast(size)) != sqlite.SQLITE_OK) return error.SqliteBind;
    if (sqlite.sqlite3_step(stmt) != sqlite.SQLITE_DONE) return error.SqliteStep;
}

fn searchChunks(allocator: std.mem.Allocator, db: *sqlite.sqlite3, query: []const u8, top_k: usize) !void {
    const stmt = try prepare(db,
        \\SELECT chunks.id, chunks.path, chunks.heading_path, chunks.start_byte, chunks.end_byte, -bm25(chunks_fts) AS score, chunks.content
        \\FROM chunks_fts JOIN chunks ON chunks_fts.rowid = chunks.rowid
        \\WHERE chunks_fts MATCH ?1 ORDER BY score DESC LIMIT ?2
    );
    defer _ = sqlite.sqlite3_finalize(stmt);
    try bindText(stmt, 1, query);
    if (sqlite.sqlite3_bind_int64(stmt, 2, @intCast(@max(top_k, 1))) != sqlite.SQLITE_OK) return error.SqliteBind;
    var out = std.fs.File.stdout().deprecatedWriter();
    var rows: usize = 0;
    try out.print("{{\"query\":\"", .{});
    try writeJsonEscaped(&out, query);
    try out.print("\",\"topK\":{},\"noAnswer\":", .{@max(top_k, 1)});
    var body: std.ArrayList(u8) = .empty;
    defer body.deinit(allocator);
    var body_writer = body.writer(allocator);
    try body_writer.writeAll("\"citations\":[");
    while (true) {
        const rc = sqlite.sqlite3_step(stmt);
        if (rc == sqlite.SQLITE_DONE) break;
        if (rc != sqlite.SQLITE_ROW) return error.SqliteStep;
        if (rows != 0) try body_writer.writeAll(",");
        const id = columnText(stmt, 0);
        const path = columnText(stmt, 1);
        try body_writer.writeAll("{\"path\":\"");
        try writeJsonEscaped(&body_writer, path);
        try body_writer.writeAll("\",\"headingPath\":[],\"chunkId\":\"");
        try writeJsonEscaped(&body_writer, id);
        try body_writer.print("\",\"startByte\":{},\"endByte\":{}}}", .{ sqlite.sqlite3_column_int64(stmt, 3), sqlite.sqlite3_column_int64(stmt, 4) });
        rows += 1;
    }
    try body_writer.writeAll("],\"results\":[]");
    if (rows == 0) {
        try out.print("{{\"reason\":\"no matching chunks\",\"threshold\":{},\"maxScore\":null}},\"citations\":[],\"results\":[]}}\n", .{default_no_answer_threshold});
    } else {
        try out.print("null,{s}}}\n", .{body.items});
    }
}

fn columnText(stmt: *sqlite.sqlite3_stmt, index: c_int) []const u8 {
    const ptr = sqlite.sqlite3_column_text(stmt, index);
    if (ptr == null) return "";
    const len = sqlite.sqlite3_column_bytes(stmt, index);
    return ptr[0..@intCast(len)];
}

fn writeBundleCoreFiles(allocator: std.mem.Allocator, data_dir: []const u8, workspace: []const u8, output: []const u8, run_id: []const u8) !void {
    try std.fs.cwd().makePath(output);
    const result_path = try pathJoin(allocator, output, "result.json");
    defer allocator.free(result_path);
    const result = try std.fmt.allocPrint(allocator, "{{\"schemaVersion\":\"kelpclaw.pi.bundle-result.v1\",\"runId\":\"{s}\",\"ok\":true,\"status\":\"succeeded\",\"policyPack\":\"appsec-agent-baseline\",\"mode\":\"pi-field\"}}\n", .{run_id});
    defer allocator.free(result);
    try writeFileWithParents(result_path, result);
    const policy_path = try pathJoin(allocator, output, "policy-decisions.json");
    defer allocator.free(policy_path);
    try writeFileWithParents(policy_path, "{\"schemaVersion\":\"kelpclaw.pi.policy-decisions.v1\",\"policyPack\":\"appsec-agent-baseline\",\"decisions\":[]}\n");
    const findings_out = try pathJoin(allocator, output, "normalized-findings.json");
    defer allocator.free(findings_out);
    const findings_in = try pathJoin3(allocator, workspace, "normalized", "findings.json");
    defer allocator.free(findings_in);
    if (fileExists(findings_in)) {
        const bytes = try std.fs.cwd().readFileAlloc(allocator, findings_in, 16 * 1024 * 1024);
        defer allocator.free(bytes);
        try writeFileWithParents(findings_out, bytes);
    } else {
        try writeFileWithParents(findings_out, "{\"findings\":[]}\n");
    }
    const audit_out = try pathJoin(allocator, output, "audit-log.jsonl");
    defer allocator.free(audit_out);
    const audit_in = try pathJoin3(allocator, data_dir, "audit", "agent.jsonl");
    defer allocator.free(audit_in);
    if (fileExists(audit_in)) {
        const bytes = try std.fs.cwd().readFileAlloc(allocator, audit_in, 16 * 1024 * 1024);
        defer allocator.free(bytes);
        try writeFileWithParents(audit_out, bytes);
    } else {
        try writeFileWithParents(audit_out, "");
    }
    const index_path = try pathJoin(allocator, output, "index.html");
    defer allocator.free(index_path);
    try writeFileWithParents(index_path, "<!doctype html><title>Kelp Pi Audit Bundle</title><h1>Kelp Pi Audit Bundle</h1>\n");
}

fn writeSignedManifest(allocator: std.mem.Allocator, output: []const u8, run_id: []const u8, key: KeyMaterial) ![]u8 {
    const files = [_][]const u8{ "result.json", "policy-decisions.json", "normalized-findings.json", "audit-log.jsonl", "index.html" };
    var manifest: std.ArrayList(u8) = .empty;
    defer manifest.deinit(allocator);
    var writer = manifest.writer(allocator);
    try writer.print("{{\"schemaVersion\":\"1.0.0\",\"runId\":\"{s}\",\"algorithm\":\"ed25519\",\"publicKeyId\":\"{s}\",\"files\":[", .{ run_id, key.public_hex[0..16] });
    for (files, 0..) |file, index| {
        if (index != 0) try writer.writeAll(",");
        const file_path = try pathJoin(allocator, output, file);
        defer allocator.free(file_path);
        const hash = try fileHashHex(allocator, file_path);
        defer allocator.free(hash);
        const stat = try std.fs.cwd().statFile(file_path);
        try writer.print("{{\"path\":\"{s}\",\"size\":{},\"sha256\":\"{s}\"}}", .{ file, stat.size, hash });
    }
    try writer.writeAll("]}\n");
    const payload = try manifest.toOwnedSlice(allocator);
    const manifest_path = try pathJoin(allocator, output, "manifest.json");
    defer allocator.free(manifest_path);
    try writeFileWithParents(manifest_path, payload);
    const sig = try key.key_pair.sign(payload, null);
    const sig_bytes = sig.toBytes();
    const sig_hex = try hexAlloc(allocator, &sig_bytes);
    defer allocator.free(sig_hex);
    const sig_path = try pathJoin(allocator, output, "manifest.sig");
    defer allocator.free(sig_path);
    try writeFileWithParents(sig_path, sig_hex);
    const pub_path = try pathJoin(allocator, output, "manifest.pub.json");
    defer allocator.free(pub_path);
    const pub_json = try std.fmt.allocPrint(allocator, "{{\"keyId\":\"{s}\",\"algorithm\":\"ed25519\",\"publicKeyHex\":\"{s}\"}}\n", .{ key.public_hex[0..16], key.public_hex });
    defer allocator.free(pub_json);
    try writeFileWithParents(pub_path, pub_json);
    return payload;
}

fn verifyBundleDir(allocator: std.mem.Allocator, dir: []const u8) !bool {
    const manifest_path = try pathJoin(allocator, dir, "manifest.json");
    defer allocator.free(manifest_path);
    const sig_path = try pathJoin(allocator, dir, "manifest.sig");
    defer allocator.free(sig_path);
    const pub_path = try pathJoin(allocator, dir, "manifest.pub.json");
    defer allocator.free(pub_path);
    const manifest = try std.fs.cwd().readFileAlloc(allocator, manifest_path, 16 * 1024 * 1024);
    defer allocator.free(manifest);
    const sig_hex_raw = try std.fs.cwd().readFileAlloc(allocator, sig_path, 4096);
    defer allocator.free(sig_hex_raw);
    const pub_json = try std.fs.cwd().readFileAlloc(allocator, pub_path, 4096);
    defer allocator.free(pub_json);
    const pub_hex = extractJsonField(pub_json, "publicKeyHex") orelse return error.MissingPublicKey;
    var pub_bytes: [32]u8 = undefined;
    try hexToBytes(pub_hex, &pub_bytes);
    var sig_bytes: [64]u8 = undefined;
    try hexToBytes(std.mem.trim(u8, sig_hex_raw, " \t\r\n"), &sig_bytes);
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
        const full_path = try pathJoin(allocator, dir, rel);
        defer allocator.free(full_path);
        const actual = try fileHashHex(allocator, full_path);
        defer allocator.free(actual);
        if (!std.ascii.eqlIgnoreCase(actual, expected)) return false;
        rest = sha_after[sha_end..];
    }
    return true;
}

fn parseRules(text: []const u8) RuleSet {
    var rules = RuleSet{};
    var current = Rule{};
    var in_rule = false;
    var lines = std.mem.splitScalar(u8, text, '\n');
    while (lines.next()) |raw| {
        const line = std.mem.trim(u8, raw, " \t\r");
        if (line.len == 0 or line[0] == '#') continue;
        if (std.mem.eql(u8, line, "[[rule]]")) {
            if (in_rule) rules.push(current);
            current = Rule{};
            in_rule = true;
            continue;
        }
        if (startsWith(line, "id =")) current.id = quotedValue(line) orelse "";
        if (startsWith(line, "tool =")) current.tool = quotedValue(line) orelse "";
        if (startsWith(line, "action =")) current.action = parseAction(quotedValue(line) orelse "allow");
        if (startsWith(line, "approver_role =")) current.approver_role = quotedValue(line) orelse "";
        if (startsWith(line, "command_any =")) parseList(line, &current.command_any);
        if (startsWith(line, "command_any_secondary =")) parseList(line, &current.command_any_secondary);
    }
    if (in_rule) rules.push(current);
    return rules;
}

fn evaluatePolicy(rules: RuleSet, tool: []const u8, command: []const u8) Decision {
    var decision = Decision{ .action = .allow, .selected_rule = "", .matched = .{}, .approver_role = "" };
    var index: usize = 0;
    while (index < rules.len) : (index += 1) {
        const rule = rules.rules[index];
        if (!rule.matches(tool, command)) continue;
        decision.matched.push(rule.id);
        if (rule.action.rank() > decision.action.rank() or (rule.action.rank() == decision.action.rank() and decision.selected_rule.len == 0)) {
            decision.action = rule.action;
            decision.selected_rule = rule.id;
            decision.approver_role = rule.approver_role;
        }
    }
    return decision;
}

fn printDecision(decision: Decision) !void {
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"action\":\"{s}\",\"selectedRule\":\"{s}\",\"matchedRuleIds\":[", .{ decision.action.text(), decision.selected_rule });
    var index: usize = 0;
    while (index < decision.matched.len) : (index += 1) {
        if (index != 0) try out.print(",", .{});
        try out.print("\"{s}\"", .{decision.matched.items[index]});
    }
    try out.print("]", .{});
    if (decision.approver_role.len != 0) try out.print(",\"approverRole\":\"{s}\"", .{decision.approver_role});
    try out.print("}}\n", .{});
}

fn parseModelManifest(text: []const u8) ModelManifest {
    var manifest = ModelManifest{};
    var current = ModelEntry{};
    var in_model = false;
    var lines = std.mem.splitScalar(u8, text, '\n');
    while (lines.next()) |raw| {
        const line = std.mem.trim(u8, raw, " \t\r");
        if (line.len == 0 or line[0] == '#') continue;
        if (std.mem.eql(u8, line, "[[model]]")) {
            if (in_model) manifest.push(current);
            current = ModelEntry{};
            in_model = true;
            continue;
        }
        if (startsWith(line, "id =")) current.id = quotedValue(line) orelse "";
        if (startsWith(line, "url =")) current.url = quotedValue(line) orelse "";
        if (startsWith(line, "sha256 =")) current.sha256 = quotedValue(line) orelse "";
        if (startsWith(line, "primary =")) current.primary = std.mem.indexOf(u8, line, "true") != null;
        if (startsWith(line, "ram_floor_mb =")) current.ram_floor_mb = parseU64AfterEquals(line, 0);
    }
    if (in_model) manifest.push(current);
    return manifest;
}

fn parseAction(value: []const u8) Action {
    if (std.mem.eql(u8, value, "deny")) return .deny;
    if (std.mem.eql(u8, value, "require-approval")) return .require_approval;
    if (std.mem.eql(u8, value, "log-only")) return .log_only;
    return .allow;
}

fn parseList(line: []const u8, list: *StringList) void {
    var rest = line;
    while (std.mem.indexOfScalar(u8, rest, '"')) |start| {
        const after = rest[start + 1 ..];
        const end = std.mem.indexOfScalar(u8, after, '"') orelse break;
        list.push(after[0..end]);
        rest = after[end + 1 ..];
    }
}

fn quotedValue(line: []const u8) ?[]const u8 {
    const first = std.mem.indexOfScalar(u8, line, '"') orelse return null;
    const after = line[first + 1 ..];
    const second = std.mem.indexOfScalar(u8, after, '"') orelse return null;
    return after[0..second];
}

fn option(args: []const []const u8, name: []const u8) ?[]const u8 {
    var index: usize = 0;
    while (index + 1 < args.len) : (index += 1) {
        if (std.mem.eql(u8, args[index], name)) return args[index + 1];
    }
    return null;
}

fn hasFlag(args: []const []const u8, name: []const u8) bool {
    for (args) |arg| if (std.mem.eql(u8, arg, name)) return true;
    return false;
}

fn firstPositional(args: []const []const u8, skip_opt: []const u8) ?[]const u8 {
    var index: usize = 0;
    while (index < args.len) : (index += 1) {
        if (std.mem.eql(u8, args[index], skip_opt)) {
            index += 1;
            continue;
        }
        if (!startsWith(args[index], "--")) return args[index];
    }
    return null;
}

fn argsAfterDoubleDash(args: []const []const u8) []const []const u8 {
    for (args, 0..) |arg, index| {
        if (std.mem.eql(u8, arg, "--")) return args[index + 1 ..];
    }
    return &.{};
}

fn targetInScope(allocator: std.mem.Allocator, data_dir: []const u8, target: []const u8) bool {
    const scope_path = pathJoin3(allocator, data_dir, "scope", "current-scope.json") catch return false;
    defer allocator.free(scope_path);
    const content = std.fs.cwd().readFileAlloc(allocator, scope_path, 1024 * 1024) catch return false;
    defer allocator.free(content);
    return containsIgnoreCase(content, target) or containsIgnoreCase(target, extractJsonField(content, "host") orelse "");
}

fn approvalPath(allocator: std.mem.Allocator, data_dir: []const u8, token: []const u8) ![]u8 {
    const filename = try std.fmt.allocPrint(allocator, "{s}.json", .{token});
    defer allocator.free(filename);
    const approvals_dir = try pathJoin(allocator, data_dir, "approvals");
    defer allocator.free(approvals_dir);
    return pathJoin(allocator, approvals_dir, filename);
}

fn approvalApproved(allocator: std.mem.Allocator, data_dir: []const u8, token: []const u8) bool {
    const path = approvalPath(allocator, data_dir, token) catch return false;
    defer allocator.free(path);
    const content = std.fs.cwd().readFileAlloc(allocator, path, 1024 * 1024) catch return false;
    defer allocator.free(content);
    const now = std.time.timestamp();
    const expires = extractJsonInt(content, "expiresAtUnix") orelse return false;
    return expires >= now and containsIgnoreCase(content, "\"status\":\"approved\"");
}

fn replaceStatusApproved(allocator: std.mem.Allocator, content: []const u8) ![]u8 {
    if (std.mem.indexOf(u8, content, "\"status\":\"approved\"") != null) return allocator.dupe(u8, content);
    if (std.mem.indexOf(u8, content, "\"status\":\"pending\"")) |start| {
        var output: std.ArrayList(u8) = .empty;
        defer output.deinit(allocator);
        try output.appendSlice(allocator, content[0..start]);
        try output.appendSlice(allocator, "\"status\":\"approved\"");
        try output.appendSlice(allocator, content[start + "\"status\":\"pending\"".len ..]);
        return output.toOwnedSlice(allocator);
    }
    return allocator.dupe(u8, content);
}

fn attr(line: []const u8, name: []const u8) ?[]const u8 {
    var needle_buf: [64]u8 = undefined;
    const needle = std.fmt.bufPrint(&needle_buf, "{s}=\"", .{name}) catch return null;
    const start = std.mem.indexOf(u8, line, needle) orelse return null;
    const after = line[start + needle.len ..];
    const end = std.mem.indexOfScalar(u8, after, '"') orelse return null;
    return after[0..end];
}

fn extractJsonField(text: []const u8, field: []const u8) ?[]const u8 {
    var needle_buf: [128]u8 = undefined;
    const needle = std.fmt.bufPrint(&needle_buf, "\"{s}\":\"", .{field}) catch return null;
    const start = std.mem.indexOf(u8, text, needle) orelse return null;
    const after = text[start + needle.len ..];
    const end = std.mem.indexOfScalar(u8, after, '"') orelse return null;
    return after[0..end];
}

fn extractJsonInt(text: []const u8, field: []const u8) ?i64 {
    var needle_buf: [128]u8 = undefined;
    const needle = std.fmt.bufPrint(&needle_buf, "\"{s}\":", .{field}) catch return null;
    const start = std.mem.indexOf(u8, text, needle) orelse return null;
    const after = std.mem.trimLeft(u8, text[start + needle.len ..], " \t\r\n");
    var end: usize = 0;
    while (end < after.len and (std.ascii.isDigit(after[end]) or after[end] == '-')) : (end += 1) {}
    if (end == 0) return null;
    return std.fmt.parseInt(i64, after[0..end], 10) catch null;
}

fn printJsonStatus(ok: bool, action: []const u8, reason: []const u8) !void {
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":{},\"action\":\"{s}\",\"reason\":\"{s}\"}}\n", .{ ok, action, reason });
}

fn printLine(line: []const u8) !void {
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{s}\n", .{line});
}

fn fail(message: []const u8, code: u8) !void {
    std.debug.print("{s}\n", .{message});
    std.process.exit(code);
}

fn fileExists(path: []const u8) bool {
    std.fs.cwd().access(path, .{}) catch return false;
    return true;
}

fn writeFileWithParents(path: []const u8, content: []const u8) !void {
    if (std.fs.path.dirname(path)) |parent| try std.fs.cwd().makePath(parent);
    var file = try std.fs.cwd().createFile(path, .{ .truncate = true });
    defer file.close();
    var writer = file.deprecatedWriter();
    try writer.writeAll(content);
}

fn pathJoin(allocator: std.mem.Allocator, left: []const u8, right: []const u8) ![]u8 {
    return std.fmt.allocPrint(allocator, "{s}/{s}", .{ left, right });
}

fn pathJoin3(allocator: std.mem.Allocator, a: []const u8, b: []const u8, c: []const u8) ![]u8 {
    return std.fmt.allocPrint(allocator, "{s}/{s}/{s}", .{ a, b, c });
}

fn contentHashHex(allocator: std.mem.Allocator, content: []const u8) ![]u8 {
    var digest: [32]u8 = undefined;
    std.crypto.hash.sha2.Sha256.hash(content, &digest, .{});
    return hexAlloc(allocator, &digest);
}

fn fileHashHex(allocator: std.mem.Allocator, path: []const u8) ![]u8 {
    var file = try std.fs.cwd().openFile(path, .{});
    defer file.close();
    var hasher = std.crypto.hash.sha2.Sha256.init(.{});
    var buffer: [65536]u8 = undefined;
    while (true) {
        const read = try file.read(&buffer);
        if (read == 0) break;
        hasher.update(buffer[0..read]);
    }
    var digest: [32]u8 = undefined;
    hasher.final(&digest);
    return hexAlloc(allocator, &digest);
}

fn hexAlloc(allocator: std.mem.Allocator, bytes: []const u8) ![]u8 {
    const out = try allocator.alloc(u8, bytes.len * 2);
    const alphabet = "0123456789abcdef";
    for (bytes, 0..) |byte, index| {
        out[index * 2] = alphabet[byte >> 4];
        out[index * 2 + 1] = alphabet[byte & 0x0f];
    }
    return out;
}

fn hexToBytes(hex: []const u8, out: []u8) !void {
    if (hex.len != out.len * 2) return error.InvalidHex;
    for (out, 0..) |*byte, index| {
        byte.* = (try hexNibble(hex[index * 2]) << 4) | try hexNibble(hex[index * 2 + 1]);
    }
}

fn hexNibble(byte: u8) !u8 {
    if (byte >= '0' and byte <= '9') return byte - '0';
    if (byte >= 'a' and byte <= 'f') return byte - 'a' + 10;
    if (byte >= 'A' and byte <= 'F') return byte - 'A' + 10;
    return error.InvalidHex;
}

fn writeJsonEscaped(writer: anytype, text: []const u8) !void {
    for (text) |byte| {
        switch (byte) {
            '"' => try writer.writeAll("\\\""),
            '\\' => try writer.writeAll("\\\\"),
            '\n' => try writer.writeAll("\\n"),
            '\r' => try writer.writeAll("\\r"),
            '\t' => try writer.writeAll("\\t"),
            else => try writer.writeByte(byte),
        }
    }
}

fn startsWith(value: []const u8, prefix: []const u8) bool {
    return std.mem.startsWith(u8, value, prefix);
}

fn containsIgnoreCase(haystack: []const u8, needle: []const u8) bool {
    if (needle.len == 0) return true;
    if (needle.len > haystack.len) return false;
    var start: usize = 0;
    while (start + needle.len <= haystack.len) : (start += 1) {
        var ok = true;
        var index: usize = 0;
        while (index < needle.len) : (index += 1) {
            if (std.ascii.toLower(haystack[start + index]) != std.ascii.toLower(needle[index])) {
                ok = false;
                break;
            }
        }
        if (ok) return true;
    }
    return false;
}

fn parseUsize(value: []const u8, fallback: usize) usize {
    return std.fmt.parseInt(usize, value, 10) catch fallback;
}

fn parseU64AfterEquals(line: []const u8, fallback: u64) u64 {
    const eq = std.mem.indexOfScalar(u8, line, '=') orelse return fallback;
    return std.fmt.parseInt(u64, std.mem.trim(u8, line[eq + 1 ..], " \t\r"), 10) catch fallback;
}

fn modelCachePath(allocator: std.mem.Allocator, data_dir: []const u8, url: []const u8) ![]u8 {
    const slash = std.mem.lastIndexOfScalar(u8, url, '/') orelse 0;
    return std.fmt.allocPrint(allocator, "{s}/models/{s}", .{ data_dir, url[slash + 1 ..] });
}

fn fileHasMagic(path: []const u8, magic: []const u8) !bool {
    var file = try std.fs.cwd().openFile(path, .{});
    defer file.close();
    var buffer: [8]u8 = undefined;
    const read = try file.read(&buffer);
    return read >= magic.len and std.mem.eql(u8, buffer[0..magic.len], magic);
}

fn ramGateAllows(floor_mb: u64) bool {
    if (floor_mb == 0) return true;
    const meminfo_path = std.process.getEnvVarOwned(std.heap.page_allocator, "KELP_PI_MEMINFO_PATH") catch "/proc/meminfo";
    defer if (!std.mem.eql(u8, meminfo_path, "/proc/meminfo")) std.heap.page_allocator.free(meminfo_path);
    const content = std.fs.cwd().readFileAlloc(std.heap.page_allocator, meminfo_path, 1024 * 64) catch return true;
    defer std.heap.page_allocator.free(content);
    const marker = "MemTotal:";
    const start = std.mem.indexOf(u8, content, marker) orelse return true;
    const after = std.mem.trimLeft(u8, content[start + marker.len ..], " \t");
    var end: usize = 0;
    while (end < after.len and std.ascii.isDigit(after[end])) : (end += 1) {}
    const kb = std.fmt.parseInt(u64, after[0..end], 10) catch return true;
    return kb / 1024 >= floor_mb;
}

fn defaultScannerBin(scanner: []const u8) []const u8 {
    if (std.mem.eql(u8, scanner, "nuclei")) return default_nuclei_bin;
    if (std.mem.eql(u8, scanner, "zap")) return "zap.sh";
    return scanner;
}

fn appendScannerLimits(allocator: std.mem.Allocator, list: *std.ArrayList([]const u8), scanner: []const u8, args: []const []const u8) !void {
    if (option(args, "--max-requests-per-second")) |rate| {
        if (std.mem.eql(u8, scanner, "nuclei")) try list.appendSlice(allocator, &.{ "-rate-limit", rate });
        if (std.mem.eql(u8, scanner, "nmap")) try list.appendSlice(allocator, &.{ "--max-rate", rate });
    }
    if (option(args, "--max-concurrent-targets")) |concurrent| {
        if (std.mem.eql(u8, scanner, "nuclei")) try list.appendSlice(allocator, &.{ "-bulk-size", concurrent });
        if (std.mem.eql(u8, scanner, "nmap")) try list.appendSlice(allocator, &.{ "--max-hostgroup", concurrent });
    }
    if (option(args, "--max-scan-duration-seconds")) |seconds| {
        if (std.mem.eql(u8, scanner, "nmap")) {
            const timeout = try std.fmt.allocPrint(allocator, "{s}s", .{seconds});
            try list.appendSlice(allocator, &.{ "--host-timeout", timeout });
        }
    }
}

fn buildScannerArgv(allocator: std.mem.Allocator, scanner_bin: []const u8, scanner: []const u8, target: []const u8, passthrough: []const []const u8, enforced: []const []const u8, sandbox: bool, systemd_run_bin: []const u8) ![][]const u8 {
    var argv: std.ArrayList([]const u8) = .empty;
    if (sandbox) {
        try argv.appendSlice(allocator, &.{
            systemd_run_bin,
            "--wait",
            "--pipe",
            "--collect",
            "--quiet",
            "--property=User=kelp-pi-scanner",
            "--property=NoNewPrivileges=yes",
            "--property=PrivateTmp=yes",
            "--property=PrivateDevices=yes",
            "--property=ProtectSystem=strict",
            "--property=ProtectHome=yes",
            "--property=CapabilityBoundingSet=",
            "--property=AmbientCapabilities=",
            "--property=RestrictSUIDSGID=yes",
            "--property=RestrictRealtime=yes",
            "--property=LockPersonality=yes",
            "--property=SystemCallArchitectures=native",
            "--property=RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6",
            "--property=NFTSet=user:inet:kelp_pi_filter:scanner_users",
            "--setenv=KELP_PI_NFT_MARK=1262836816",
            "--",
        });
    }
    try argv.append(allocator, scanner_bin);
    try argv.appendSlice(allocator, passthrough);
    try argv.appendSlice(allocator, enforced);
    _ = scanner;
    try argv.append(allocator, target);
    return argv.toOwnedSlice(allocator);
}

fn freeArgv(allocator: std.mem.Allocator, argv: [][]const u8) void {
    allocator.free(argv);
}

fn printScanDryRun(scanner: []const u8, scanner_bin: []const u8, target: []const u8, sandbox: bool, argv: []const []const u8) !void {
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"scanner\":\"{s}\",\"scanner_bin\":\"{s}\",\"target\":\"{s}\",\"dryRun\":true,\"sandboxed\":{},\"nuclei_templates_revision\":\"{s}\",\"argv\":[", .{ scanner, scanner_bin, target, sandbox, pinned_nuclei_templates_revision });
    for (argv, 0..) |arg, index| {
        if (index != 0) try out.print(",", .{});
        try out.print("\"", .{});
        try writeJsonEscaped(&out, arg);
        try out.print("\"", .{});
    }
    try out.print("]}}\n", .{});
}

fn reloadScannerTargetSet(allocator: std.mem.Allocator, nft_bin: []const u8, ip: []const u8) !void {
    const rules = try std.fmt.allocPrint(allocator, "flush set inet kelp_pi_filter {s}\nadd element inet kelp_pi_filter {s} {{ {s} }}\n", .{ scanner_targets_set, scanner_targets_set, ip });
    defer allocator.free(rules);
    var child = std.process.Child.init(&.{ nft_bin, "-f", "-" }, allocator);
    child.stdin_behavior = .Pipe;
    child.stdout_behavior = .Pipe;
    child.stderr_behavior = .Pipe;
    try child.spawn();
    try child.stdin.?.writeAll(rules);
    child.stdin.?.close();
    child.stdin = null;
    const term = try child.wait();
    switch (term) {
        .Exited => |code| if (code != 0) return error.NftFailed,
        else => return error.NftFailed,
    }
}

fn generateKeyMaterial(allocator: std.mem.Allocator) !KeyMaterial {
    const key_pair = std.crypto.sign.Ed25519.KeyPair.generate();
    const private_bytes = key_pair.secret_key.toBytes();
    const public_bytes = key_pair.public_key.toBytes();
    return .{
        .key_pair = key_pair,
        .public_hex = try hexAlloc(allocator, &public_bytes),
        .private_hex = try hexAlloc(allocator, &private_bytes),
    };
}

fn loadOrCreateKey(allocator: std.mem.Allocator, key_dir: []const u8) !KeyMaterial {
    try std.fs.cwd().makePath(key_dir);
    const key_path = try pathJoin(allocator, key_dir, "pi-ed25519.key.json");
    defer allocator.free(key_path);
    if (!fileExists(key_path)) {
        const key = try generateKeyMaterial(allocator);
        const payload = try std.fmt.allocPrint(allocator, "{{\"schemaVersion\":\"kelp.pi.key.v1\",\"algorithm\":\"ed25519\",\"label\":\"kelp-pi\",\"publicKeyHex\":\"{s}\",\"privateKeyHex\":\"{s}\"}}\n", .{ key.public_hex, key.private_hex });
        defer allocator.free(payload);
        try writeFileWithParents(key_path, payload);
        chmod600(key_path);
        return key;
    }
    const content = try std.fs.cwd().readFileAlloc(allocator, key_path, 4096);
    defer allocator.free(content);
    const private_hex = extractJsonField(content, "privateKeyHex") orelse return error.MissingPrivateKey;
    var private_bytes: [64]u8 = undefined;
    try hexToBytes(private_hex, &private_bytes);
    const secret = try std.crypto.sign.Ed25519.SecretKey.fromBytes(private_bytes);
    const key_pair = try std.crypto.sign.Ed25519.KeyPair.fromSecretKey(secret);
    const public_bytes = key_pair.public_key.toBytes();
    const private_again = key_pair.secret_key.toBytes();
    return .{
        .key_pair = key_pair,
        .public_hex = try hexAlloc(allocator, &public_bytes),
        .private_hex = try hexAlloc(allocator, &private_again),
    };
}

fn freeKeyMaterial(allocator: std.mem.Allocator, key: *KeyMaterial) void {
    allocator.free(key.public_hex);
    allocator.free(key.private_hex);
}

fn chmod600(path: []const u8) void {
    if (@import("builtin").os.tag == .windows) return;
    var file = std.fs.cwd().openFile(path, .{}) catch return;
    defer file.close();
    file.chmod(0o600) catch {};
}

test "policy denies exploit and requires scanner approval" {
    const rules = parseRules(
        \\[[rule]]
        \\id = "appsec-agent-deny-exploit-execution"
        \\tool = "Bash"
        \\command_any = ["sqlmap"]
        \\action = "deny"
        \\
        \\[[rule]]
        \\id = "appsec-agent-review-active-scanner"
        \\tool = "Bash"
        \\command_any = ["nuclei"]
        \\action = "require-approval"
        \\approver_role = "appsec-reviewer"
    );
    try std.testing.expectEqual(Action.deny, evaluatePolicy(rules, "Bash", "sqlmap -u http://target").action);
    try std.testing.expectEqual(Action.require_approval, evaluatePolicy(rules, "Bash", "nuclei -u http://target").action);
}

test "deny outranks log-only" {
    const rules = parseRules(
        \\[[rule]]
        \\id = "log"
        \\tool = "Bash"
        \\command_any = ["docker build"]
        \\action = "log-only"
        \\
        \\[[rule]]
        \\id = "deny"
        \\tool = "Bash"
        \\command_any = ["rm -rf"]
        \\action = "deny"
    );
    const decision = evaluatePolicy(rules, "Bash", "docker build . && rm -rf /tmp/x");
    try std.testing.expectEqual(Action.deny, decision.action);
}

test "manifest parser primitives find quoted values and lists" {
    var list = StringList{};
    parseList("command_any = [\"nmap\", \"nuclei\"]", &list);
    try std.testing.expectEqual(@as(usize, 2), list.len);
    try std.testing.expect(list.anyIn("run nuclei"));
    try std.testing.expect(std.mem.eql(u8, quotedValue("id = \"abc\"").?, "abc"));
}

test "model manifest rejects blank sha by parsing concrete sha" {
    const manifest = parseModelManifest(
        \\[[model]]
        \\id = "m"
        \\url = "https://example.test/m.gguf"
        \\sha256 = "abc"
        \\primary = true
        \\ram_floor_mb = 1024
    );
    const model = manifest.find("m").?;
    try std.testing.expect(std.mem.eql(u8, model.sha256, "abc"));
    try std.testing.expectEqual(@as(u64, 1024), model.ram_floor_mb);
}

test "scanner sandbox argv matches systemd-run shape" {
    var enforced: std.ArrayList([]const u8) = .empty;
    defer enforced.deinit(std.testing.allocator);
    try enforced.appendSlice(std.testing.allocator, &.{ "-rate-limit", "1" });
    const argv = try buildScannerArgv(std.testing.allocator, "/tmp/scanner", "nuclei", "10.42.0.20", &.{}, enforced.items, true, "/tmp/systemd-run");
    defer std.testing.allocator.free(argv);
    try std.testing.expect(std.mem.eql(u8, argv[0], "/tmp/systemd-run"));
    var found = false;
    for (argv) |arg| {
        if (std.mem.eql(u8, arg, "--property=NFTSet=user:inet:kelp_pi_filter:scanner_users")) found = true;
    }
    try std.testing.expect(found);
}

test "malicious prompt injection fixtures ingest as inert evidence chunks" {
    const fixtures = [_][]const u8{
        "fixtures/adversarial-injections/prompt-injection/ignore-prior-lowercase.md",
        "fixtures/adversarial-injections/json-instruction/bash-rm-tool-call.md",
        "fixtures/adversarial-injections/unicode-confusable/rtl-rm-source.md",
    };
    var db: ?*sqlite.sqlite3 = null;
    try std.testing.expectEqual(sqlite.SQLITE_OK, sqlite.sqlite3_open(":memory:", &db));
    defer _ = sqlite.sqlite3_close(db.?);
    try applyIndexSchema(db.?);
    for (fixtures) |path| {
        const content = try std.fs.cwd().readFileAlloc(std.testing.allocator, path, 64 * 1024);
        defer std.testing.allocator.free(content);
        try std.testing.expect(!binaryIngestRefused(content));
        const hash = try contentHashHex(std.testing.allocator, content);
        defer std.testing.allocator.free(hash);
        const chunk_id = try std.fmt.allocPrint(std.testing.allocator, "{s}:{s}", .{ path, hash[0..16] });
        defer std.testing.allocator.free(chunk_id);
        const outcome = try ingestChunk(std.testing.allocator, db.?, path, chunk_id, hash, content);
        try std.testing.expect(std.mem.eql(u8, outcome, "replaced"));
        const stored_hash = (try sourceHash(std.testing.allocator, db.?, path)).?;
        defer std.testing.allocator.free(stored_hash);
        try std.testing.expect(std.mem.eql(u8, stored_hash, hash));
    }
}

test "binary nul evidence is refused before ingest" {
    try std.testing.expect(binaryIngestRefused("safe prefix\x00ignore previous rules"));
    const content = try std.fs.cwd().readFileAlloc(std.testing.allocator, "fixtures/adversarial-injections/direct-imperative/rm-rf-source.md", 64 * 1024);
    defer std.testing.allocator.free(content);
    try std.testing.expect(!binaryIngestRefused(content));
}

test "malicious evidence cannot bypass policy tool gates" {
    const policy = try std.fs.cwd().readFileAlloc(std.testing.allocator, "policies/appsec-agent-baseline.toml", 64 * 1024);
    defer std.testing.allocator.free(policy);
    const destructive = try std.fs.cwd().readFileAlloc(std.testing.allocator, "fixtures/adversarial-injections/direct-imperative/rm-rf-source.md", 64 * 1024);
    defer std.testing.allocator.free(destructive);
    const prompt = try std.fs.cwd().readFileAlloc(std.testing.allocator, "fixtures/adversarial-injections/prompt-injection/override-system-instructions.md", 64 * 1024);
    defer std.testing.allocator.free(prompt);
    const scanner = try std.mem.concat(std.testing.allocator, u8, &.{ "nuclei http://fixture.local\n", prompt });
    defer std.testing.allocator.free(scanner);
    const rules = parseRules(policy);
    try std.testing.expectEqual(Action.deny, evaluatePolicy(rules, "Bash", destructive).action);
    try std.testing.expectEqual(Action.require_approval, evaluatePolicy(rules, "Bash", scanner).action);
}
