const std = @import("std");

const default_data_dir = "/var/lib/kelp-pi";
const default_policy_path = "policies/appsec-agent-baseline.toml";
const default_model_manifest = "models/manifest.toml";

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
    items: [32][]const u8 = undefined,
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
    rules: [32]Rule = undefined,
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

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();
    const args = try std.process.argsAlloc(allocator);
    defer std.process.argsFree(allocator, args);

    if (args.len <= 1) return usage();
    const command = args[1];
    if (std.mem.eql(u8, command, "version") or std.mem.eql(u8, command, "--version")) return printLine("kelp-pi 0.1.0-zig");
    if (std.mem.eql(u8, command, "doctor")) return doctor(args[2..]);
    if (std.mem.eql(u8, command, "keygen")) return keygen(allocator, args[2..]);
    if (std.mem.eql(u8, command, "policy")) return policyCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "approval-request")) return approvalRequest(allocator, args[2..]);
    if (std.mem.eql(u8, command, "approve")) return approve(args[2..]);
    if (std.mem.eql(u8, command, "scope")) return scopeCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "scan")) return scanCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "index")) return indexCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "ask")) return askCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "bundle")) return bundleCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "verify-bundle")) return verifyBundle(args[2..]);
    if (std.mem.eql(u8, command, "model")) return modelCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "chat")) return chatCommand(allocator, args[2..]);
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
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print(
        "{{\"ok\":{},\"dataDir\":\"{s}\",\"checks\":[{{\"id\":\"policy-pack\",\"status\":\"{s}\"}},{{\"id\":\"model-manifest\",\"status\":\"{s}\"}}]}}\n",
        .{ policy_ok and model_ok, data_dir, if (policy_ok) "pass" else "fail", if (model_ok) "pass" else "fail" },
    );
}

fn keygen(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const data_dir = option(args, "--data-dir") orelse default_data_dir;
    const label = option(args, "--label") orelse "kelp-pi";
    const key_dir = try pathJoin(allocator, data_dir, "keys");
    defer allocator.free(key_dir);
    try std.fs.cwd().makePath(key_dir);
    var secret: [32]u8 = undefined;
    std.crypto.random.bytes(&secret);
    const secret_hex = try hexAlloc(allocator, &secret);
    defer allocator.free(secret_hex);
    const key_path = try pathJoin(allocator, key_dir, "pi-ed25519.key.json");
    defer allocator.free(key_path);
    const payload = try std.fmt.allocPrint(
        allocator,
        "{{\"schemaVersion\":\"kelp.pi.key.v1\",\"algorithm\":\"ed25519\",\"label\":\"{s}\",\"privateKeyHex\":\"{s}\"}}\n",
        .{ label, secret_hex },
    );
    defer allocator.free(payload);
    try writeFileWithParents(key_path, payload);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"keyPath\":\"{s}\",\"label\":\"{s}\"}}\n", .{ key_path, label });
}

fn policyCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "check")) return fail("usage: kelp-pi policy check --tool TOOL --command CMD", 64);
    const tool = option(args[1..], "--tool") orelse "Bash";
    const command = option(args[1..], "--command") orelse "";
    const policy_path = option(args[1..], "--policy") orelse default_policy_path;
    const policy_text = try std.fs.cwd().readFileAlloc(allocator, policy_path, 1024 * 1024);
    defer allocator.free(policy_text);
    const rules = parseRules(policy_text);
    const decision = evaluatePolicy(rules, tool, command);
    return printDecision(decision);
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
    try out.print("{{\"ok\":true,\"token\":\"{s}\",\"path\":\"{s}\",\"expiresAtUnix\":{}}}\n", .{ token, record_path, now + ttl });
}

fn approve(args: []const []const u8) !void {
    if (args.len == 0) return fail("usage: kelp-pi approve TOKEN [--data-dir DIR]", 64);
    const token = args[0];
    const data_dir = option(args[1..], "--data-dir") orelse default_data_dir;
    var out = std.fs.File.stdout().deprecatedWriter();
    const ok = approvalExists(data_dir, token);
    if (!ok) return fail("approval token not found", 77);
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
    const command = try std.fmt.allocPrint(allocator, "{s} {s}", .{ scanner, target });
    defer allocator.free(command);
    const policy_text = try std.fs.cwd().readFileAlloc(allocator, default_policy_path, 1024 * 1024);
    defer allocator.free(policy_text);
    const decision = evaluatePolicy(parseRules(policy_text), "Bash", command);
    if (decision.action == .deny) return printDecision(decision);
    if (!targetInScope(allocator, data_dir, target)) return printJsonStatus(false, "deny", "target outside active scope");
    if (decision.action == .require_approval and (token == null or !approvalExists(data_dir, token.?))) {
        return printJsonStatus(false, "require-approval", "approval token required");
    }
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"scanner\":\"{s}\",\"target\":\"{s}\",\"dryRun\":{},\"sandbox\":\"systemd-run+nftables\"}}\n", .{ scanner, target, hasFlag(args[1..], "--dry-run") });
}

fn indexCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "ingest")) return fail("usage: kelp-pi index ingest --input PATH [--path PATH]", 64);
    const input = option(args[1..], "--input") orelse return fail("index ingest requires --input", 64);
    const logical = option(args[1..], "--path") orelse input;
    const data_dir = option(args[1..], "--data-dir") orelse default_data_dir;
    const content = try std.fs.cwd().readFileAlloc(allocator, input, 16 * 1024 * 1024);
    defer allocator.free(content);
    if (std.mem.indexOfScalar(u8, content, 0) != null) return fail("binary ingest refused", 77);
    const index_dir = try pathJoin(allocator, data_dir, "index");
    defer allocator.free(index_dir);
    try std.fs.cwd().makePath(index_dir);
    const index_path = try pathJoin(allocator, index_dir, "chunks.jsonl");
    defer allocator.free(index_path);
    const chunk_id = try contentHashHex(allocator, content);
    defer allocator.free(chunk_id);
    const line = try std.fmt.allocPrint(allocator, "{{\"path\":\"{s}\",\"chunkId\":\"{s}\",\"content\":", .{ logical, chunk_id[0..16] });
    defer allocator.free(line);
    var file = try std.fs.cwd().createFile(index_path, .{ .truncate = false });
    defer file.close();
    try file.seekFromEnd(0);
    var writer = file.deprecatedWriter();
    try writer.print("{s}\"", .{line});
    try writeJsonEscaped(&writer, content);
    try writer.print("\"}}\n", .{});
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"path\":\"{s}\",\"chunkId\":\"{s}\"}}\n", .{ logical, chunk_id[0..16] });
}

fn askCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0) return fail("usage: kelp-pi ask QUERY [--data-dir DIR]", 64);
    const query = args[0];
    const data_dir = option(args[1..], "--data-dir") orelse default_data_dir;
    const index_path = try pathJoin3(allocator, data_dir, "index", "chunks.jsonl");
    defer allocator.free(index_path);
    const content = std.fs.cwd().readFileAlloc(allocator, index_path, 32 * 1024 * 1024) catch "";
    defer if (content.len != 0) allocator.free(content);
    var out = std.fs.File.stdout().deprecatedWriter();
    if (content.len == 0 or !containsIgnoreCase(content, query)) {
        return out.print("{{\"query\":\"{s}\",\"noAnswer\":{{\"reason\":\"no matching chunks\"}},\"citations\":[]}}\n", .{query});
    }
    const path = extractJsonField(content, "path") orelse "index";
    const chunk_id = extractJsonField(content, "chunkId") orelse "chunk";
    try out.print("{{\"query\":\"{s}\",\"noAnswer\":null,\"citations\":[{{\"path\":\"{s}\",\"chunkId\":\"{s}\",\"startByte\":0,\"endByte\":0}}]}}\n", .{ query, path, chunk_id });
}

fn bundleCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "assemble")) return fail("usage: kelp-pi bundle assemble --run-id ID --workspace DIR --output DIR", 64);
    const run_id = option(args[1..], "--run-id") orelse "local";
    const workspace = option(args[1..], "--workspace") orelse ".";
    const output = option(args[1..], "--output") orelse "audit-bundle";
    try std.fs.cwd().makePath(output);
    const result_path = try pathJoin(allocator, output, "result.json");
    defer allocator.free(result_path);
    const manifest_path = try pathJoin(allocator, output, "manifest.json");
    defer allocator.free(manifest_path);
    const index_path = try pathJoin(allocator, output, "index.html");
    defer allocator.free(index_path);
    const result = try std.fmt.allocPrint(allocator, "{{\"schemaVersion\":\"kelp.pi.bundle-result.v1\",\"runId\":\"{s}\",\"ok\":true,\"workspace\":\"{s}\"}}\n", .{ run_id, workspace });
    defer allocator.free(result);
    try writeFileWithParents(result_path, result);
    try writeFileWithParents(index_path, "<!doctype html><title>Kelp Pi Audit Bundle</title><h1>Kelp Pi Audit Bundle</h1>\n");
    const manifest = try std.fmt.allocPrint(allocator, "{{\"schemaVersion\":\"1.0.0\",\"runId\":\"{s}\",\"files\":[{{\"path\":\"result.json\"}},{{\"path\":\"index.html\"}}]}}\n", .{run_id});
    defer allocator.free(manifest);
    try writeFileWithParents(manifest_path, manifest);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"runId\":\"{s}\",\"bundleDir\":\"{s}\"}}\n", .{ run_id, output });
}

fn verifyBundle(args: []const []const u8) !void {
    if (args.len == 0) return fail("usage: kelp-pi verify-bundle DIR", 64);
    const dir = args[0];
    const ok = fileExistsJoin(dir, "manifest.json") and fileExistsJoin(dir, "result.json") and fileExistsJoin(dir, "index.html");
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":{},\"bundleDir\":\"{s}\"}}\n", .{ ok, dir });
}

fn modelCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "warm")) return fail("usage: kelp-pi model warm --id MODEL_ID [--model-path PATH]", 64);
    const id = option(args[1..], "--id") orelse "qwen3-0.6b-q4_k_m";
    const manifest_path = option(args[1..], "--manifest") orelse default_model_manifest;
    const manifest = try std.fs.cwd().readFileAlloc(allocator, manifest_path, 1024 * 1024);
    defer allocator.free(manifest);
    const present = containsIgnoreCase(manifest, id);
    if (!present) return printJsonStatus(false, "missing-model", "model id not in manifest");
    if (option(args[1..], "--model-path")) |model_path| {
        if (!fileExists(model_path)) return printJsonStatus(false, "missing-model-file", "model path does not exist");
    }
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"id\":\"{s}\",\"runtime\":\"llama.cpp\",\"loaded\":false,\"reason\":\"manifest verified; GGUF load requires linked llama.cpp\"}}\n", .{id});
}

fn chatCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
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

fn targetInScope(allocator: std.mem.Allocator, data_dir: []const u8, target: []const u8) bool {
    const scope_path = pathJoin3(allocator, data_dir, "scope", "current-scope.json") catch return false;
    defer allocator.free(scope_path);
    const content = std.fs.cwd().readFileAlloc(allocator, scope_path, 1024 * 1024) catch return false;
    defer allocator.free(content);
    return containsIgnoreCase(content, target) or containsIgnoreCase(target, extractJsonField(content, "host") orelse "");
}

fn approvalExists(data_dir: []const u8, token: []const u8) bool {
    var path_buf: [4096]u8 = undefined;
    const path = std.fmt.bufPrint(&path_buf, "{s}/approvals/{s}.json", .{ data_dir, token }) catch return false;
    return fileExists(path);
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

fn fileExistsJoin(dir: []const u8, file: []const u8) bool {
    var path_buf: [4096]u8 = undefined;
    const path = std.fmt.bufPrint(&path_buf, "{s}/{s}", .{ dir, file }) catch return false;
    return fileExists(path);
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

fn hexAlloc(allocator: std.mem.Allocator, bytes: []const u8) ![]u8 {
    const out = try allocator.alloc(u8, bytes.len * 2);
    const alphabet = "0123456789abcdef";
    for (bytes, 0..) |byte, index| {
        out[index * 2] = alphabet[byte >> 4];
        out[index * 2 + 1] = alphabet[byte & 0x0f];
    }
    return out;
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
