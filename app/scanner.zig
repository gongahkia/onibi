const std = @import("std");
const common = @import("common.zig");
const policy = @import("policy.zig");
const scope = @import("scope.zig");

const default_nuclei_bin = "/opt/kelp-pi/bin/nuclei";
const pinned_nuclei_templates_revision = "cce82b61d26bed35074cd57bc9d0aebd703a81d3";
const scanner_targets_set = "scanner_ipv4_targets";
const default_systemd_run = "systemd-run";
const default_nft_bin = "nft";

pub fn scanCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0) return common.fail("usage: kelp-pi scan <nuclei|nmap|zap> --target TARGET", 64);
    const scanner = args[0];
    const target = common.option(args[1..], "--target") orelse return common.fail("scan requires --target", 64);
    const data_dir = common.option(args[1..], "--data-dir") orelse common.default_data_dir;
    const token = common.option(args[1..], "--approval-token");
    const scanner_bin = common.option(args[1..], "--scanner-bin") orelse defaultScannerBin(scanner);
    const systemd_run_bin = common.option(args[1..], "--systemd-run-bin") orelse default_systemd_run;
    const nft_bin = common.option(args[1..], "--nft-bin") orelse default_nft_bin;
    const sandbox = common.hasFlag(args[1..], "--sandbox");
    const dry_run = common.hasFlag(args[1..], "--dry-run");
    const run_id = common.option(args[1..], "--run-id") orelse "local";
    if (!common.safePathSegment(run_id)) return common.fail("scan run id contains unsafe characters", 64);
    var workspace_alloc: ?[]u8 = null;
    defer if (workspace_alloc) |workspace_path| allocator.free(workspace_path);
    const workspace = common.option(args[1..], "--workspace") orelse blk: {
        workspace_alloc = try common.pathJoin3(allocator, data_dir, "scans", run_id);
        break :blk workspace_alloc.?;
    };

    const command = try std.fmt.allocPrint(allocator, "{s} {s}", .{ scanner, target });
    defer allocator.free(command);
    const policy_text = try std.fs.cwd().readFileAlloc(allocator, common.default_policy_path, 1024 * 1024);
    defer allocator.free(policy_text);
    const decision = policy.evaluatePolicy(policy.parseRules(policy_text), "Bash", command);
    if (decision.action == .deny) return policy.printDecision(decision);
    if (!scope.targetInScope(allocator, data_dir, target)) return common.printJsonStatus(false, "deny", "target outside active scope");
    if (decision.action == .require_approval and (token == null or !scope.approvalApproved(allocator, data_dir, token.?))) {
        return common.printJsonStatus(false, "require-approval", "approved token required");
    }

    var enforced: std.ArrayList([]const u8) = .empty;
    defer enforced.deinit(allocator);
    try appendScannerLimits(allocator, &enforced, scanner, args[1..]);
    const passthrough = common.argsAfterDoubleDash(args[1..]);
    const command_argv = try buildScannerArgv(allocator, scanner_bin, scanner, target, passthrough, enforced.items, sandbox, systemd_run_bin);
    defer freeArgv(allocator, command_argv);

    if (dry_run) {
        return printScanDryRun(scanner, scanner_bin, target, sandbox, command_argv);
    }
    if (sandbox) {
        if (common.option(args[1..], "--scanner-target-ip")) |ip| try reloadScannerTargetSet(allocator, nft_bin, ip);
    }
    const result = try std.process.Child.run(.{ .allocator = allocator, .argv = command_argv, .max_output_bytes = 10 * 1024 * 1024 });
    defer allocator.free(result.stdout);
    defer allocator.free(result.stderr);
    try persistScannerRun(allocator, workspace, run_id, scanner, target, result.stdout, result.stderr);
    const success = switch (result.term) {
        .Exited => |code| code == 0,
        else => false,
    };
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":{},\"scanner\":\"{s}\",\"target\":\"{s}\",\"runId\":\"{s}\",\"workspace\":\"{s}\",\"sandboxed\":{},\"stdoutBytes\":{},\"stderrBytes\":{}}}\n", .{ success, scanner, target, run_id, workspace, sandbox, result.stdout.len, result.stderr.len });
}

fn persistScannerRun(allocator: std.mem.Allocator, workspace: []const u8, run_id: []const u8, scanner: []const u8, target: []const u8, stdout: []const u8, stderr: []const u8) !void {
    const raw_dir = try common.pathJoin(allocator, workspace, "raw");
    defer allocator.free(raw_dir);
    try std.fs.cwd().makePath(raw_dir);
    const stdout_name = try std.fmt.allocPrint(allocator, "{s}.stdout", .{scanner});
    defer allocator.free(stdout_name);
    const stderr_name = try std.fmt.allocPrint(allocator, "{s}.stderr", .{scanner});
    defer allocator.free(stderr_name);
    const stdout_path = try common.pathJoin(allocator, raw_dir, stdout_name);
    defer allocator.free(stdout_path);
    const stderr_path = try common.pathJoin(allocator, raw_dir, stderr_name);
    defer allocator.free(stderr_path);
    try common.writeFileWithParents(stdout_path, stdout);
    try common.writeFileWithParents(stderr_path, stderr);
    const metadata_path = try common.pathJoin(allocator, workspace, "scan.json");
    defer allocator.free(metadata_path);
    var metadata: std.ArrayList(u8) = .empty;
    defer metadata.deinit(allocator);
    var metadata_writer = metadata.writer(allocator);
    try metadata_writer.print("{{\"schemaVersion\":\"kelp.pi.scan.v1\",\"runId\":\"", .{});
    try common.writeJsonEscaped(&metadata_writer, run_id);
    try metadata_writer.writeAll("\",\"scanner\":\"");
    try common.writeJsonEscaped(&metadata_writer, scanner);
    try metadata_writer.writeAll("\",\"target\":\"");
    try common.writeJsonEscaped(&metadata_writer, target);
    try metadata_writer.print("\",\"rawStdout\":\"raw/{s}\",\"rawStderr\":\"raw/{s}\"}}\n", .{ stdout_name, stderr_name });
    try common.writeFileWithParents(metadata_path, metadata.items);
    try writeNormalizedScannerFindings(allocator, workspace, scanner, stdout_name, stdout);
}

fn writeNormalizedScannerFindings(allocator: std.mem.Allocator, workspace: []const u8, scanner: []const u8, raw_stdout_name: []const u8, stdout: []const u8) !void {
    const normalized_dir = try common.pathJoin(allocator, workspace, "normalized");
    defer allocator.free(normalized_dir);
    try std.fs.cwd().makePath(normalized_dir);
    const findings_path = try common.pathJoin(allocator, normalized_dir, "findings.json");
    defer allocator.free(findings_path);
    var findings: std.ArrayList(u8) = .empty;
    defer findings.deinit(allocator);
    var writer = findings.writer(allocator);
    try writer.writeAll("{\"schemaVersion\":\"kelp.pi.normalized-findings.v1\",\"findings\":[");
    var count: usize = 0;
    if (std.mem.eql(u8, scanner, "nuclei")) {
        var lines = std.mem.splitScalar(u8, stdout, '\n');
        while (lines.next()) |raw_line| {
            const line = std.mem.trim(u8, raw_line, " \t\r");
            if (line.len == 0) continue;
            if (count != 0) try writer.writeAll(",");
            const template_id = common.extractJsonField(line, "template-id") orelse "nuclei";
            const matched_at = common.extractJsonField(line, "matched-at") orelse "";
            const severity = common.extractJsonField(line, "severity") orelse "unknown";
            const name = common.extractJsonField(line, "name") orelse template_id;
            try writer.writeAll("{\"scanner\":\"nuclei\",\"templateId\":\"");
            try common.writeJsonEscaped(&writer, template_id);
            try writer.writeAll("\",\"name\":\"");
            try common.writeJsonEscaped(&writer, name);
            try writer.writeAll("\",\"severity\":\"");
            try common.writeJsonEscaped(&writer, severity);
            try writer.writeAll("\",\"matchedAt\":\"");
            try common.writeJsonEscaped(&writer, matched_at);
            try writer.writeAll("\",\"rawPath\":\"raw/");
            try common.writeJsonEscaped(&writer, raw_stdout_name);
            try writer.writeAll("\"}");
            count += 1;
        }
    }
    try writer.writeAll("]}\n");
    try common.writeFileWithParents(findings_path, findings.items);
}

fn defaultScannerBin(scanner: []const u8) []const u8 {
    if (std.mem.eql(u8, scanner, "nuclei")) return default_nuclei_bin;
    if (std.mem.eql(u8, scanner, "zap")) return "zap.sh";
    return scanner;
}

fn appendScannerLimits(allocator: std.mem.Allocator, list: *std.ArrayList([]const u8), scanner: []const u8, args: []const []const u8) !void {
    if (common.option(args, "--max-requests-per-second")) |rate| {
        if (std.mem.eql(u8, scanner, "nuclei")) try list.appendSlice(allocator, &.{ "-rate-limit", rate });
        if (std.mem.eql(u8, scanner, "nmap")) try list.appendSlice(allocator, &.{ "--max-rate", rate });
    }
    if (common.option(args, "--max-concurrent-targets")) |concurrent| {
        if (std.mem.eql(u8, scanner, "nuclei")) try list.appendSlice(allocator, &.{ "-bulk-size", concurrent });
        if (std.mem.eql(u8, scanner, "nmap")) try list.appendSlice(allocator, &.{ "--max-hostgroup", concurrent });
    }
    if (common.option(args, "--max-scan-duration-seconds")) |seconds| {
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
        try common.writeJsonEscaped(&out, arg);
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

test "scanner fixture output persists raw and normalized findings" {
    const root = try common.testTempPath(std.testing.allocator, "scanner-persist");
    defer std.testing.allocator.free(root);
    defer std.fs.cwd().deleteTree(root) catch {};
    const workspace = try common.pathJoin(std.testing.allocator, root, "workspace");
    defer std.testing.allocator.free(workspace);
    const stdout_payload =
        "{\"template-id\":\"fixture-check\",\"matched-at\":\"http://fixture.local\",\"info\":{\"name\":\"Fixture Finding\",\"severity\":\"medium\"}}\n";
    try persistScannerRun(std.testing.allocator, workspace, "run-a", "nuclei", "http://fixture.local", stdout_payload, "fixture stderr\n");
    const stdout_path = try common.pathJoin3(std.testing.allocator, workspace, "raw", "nuclei.stdout");
    defer std.testing.allocator.free(stdout_path);
    const stderr_path = try common.pathJoin3(std.testing.allocator, workspace, "raw", "nuclei.stderr");
    defer std.testing.allocator.free(stderr_path);
    const normalized_path = try common.pathJoin3(std.testing.allocator, workspace, "normalized", "findings.json");
    defer std.testing.allocator.free(normalized_path);
    const stdout = try std.fs.cwd().readFileAlloc(std.testing.allocator, stdout_path, 64 * 1024);
    defer std.testing.allocator.free(stdout);
    const stderr = try std.fs.cwd().readFileAlloc(std.testing.allocator, stderr_path, 64 * 1024);
    defer std.testing.allocator.free(stderr);
    const normalized = try std.fs.cwd().readFileAlloc(std.testing.allocator, normalized_path, 64 * 1024);
    defer std.testing.allocator.free(normalized);
    try std.testing.expect(std.mem.indexOf(u8, stdout, "fixture-check") != null);
    try std.testing.expect(std.mem.indexOf(u8, stderr, "fixture stderr") != null);
    try std.testing.expect(std.mem.indexOf(u8, normalized, "\"templateId\":\"fixture-check\"") != null);
    try std.testing.expect(std.mem.indexOf(u8, normalized, "\"severity\":\"medium\"") != null);
}
