const std = @import("std");
const common = @import("common.zig");

pub fn approvalRequest(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const data_dir = common.option(args, "--data-dir") orelse common.default_data_dir;
    const scope_id = common.option(args, "--scope-id") orelse "default";
    const command = common.option(args, "--command") orelse "";
    const ttl_raw = common.option(args, "--ttl-seconds") orelse "300";
    const ttl = std.fmt.parseInt(i64, ttl_raw, 10) catch 300;
    var token_bytes: [16]u8 = undefined;
    std.crypto.random.bytes(&token_bytes);
    const token = try common.hexAlloc(allocator, &token_bytes);
    defer allocator.free(token);
    const approvals_dir = try common.pathJoin(allocator, data_dir, "approvals");
    defer allocator.free(approvals_dir);
    try std.fs.cwd().makePath(approvals_dir);
    const record_path_name = try std.fmt.allocPrint(allocator, "{s}.json", .{token});
    defer allocator.free(record_path_name);
    const record_path = try common.pathJoin(allocator, approvals_dir, record_path_name);
    defer allocator.free(record_path);
    const now = std.time.timestamp();
    const payload = try std.fmt.allocPrint(
        allocator,
        "{{\"token\":\"{s}\",\"scopeId\":\"{s}\",\"command\":\"{s}\",\"status\":\"pending\",\"createdAtUnix\":{},\"expiresAtUnix\":{}}}\n",
        .{ token, scope_id, command, now, now + ttl },
    );
    defer allocator.free(payload);
    try common.writeFileWithParents(record_path, payload);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"token\":\"{s}\",\"path\":\"{s}\",\"status\":\"pending\",\"expiresAtUnix\":{}}}\n", .{ token, record_path, now + ttl });
}

pub fn approve(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const data_dir = common.option(args, "--data-dir") orelse common.default_data_dir;
    const token = common.firstPositional(args, "--data-dir") orelse return common.fail("usage: kelp-pi approve TOKEN [--data-dir DIR]", 64);
    const record_path = try approvalPath(allocator, data_dir, token);
    defer allocator.free(record_path);
    const existing = std.fs.cwd().readFileAlloc(allocator, record_path, 1024 * 1024) catch return common.fail("approval token not found", 77);
    defer allocator.free(existing);
    const updated = try replaceStatusApproved(allocator, existing);
    defer allocator.free(updated);
    try common.writeFileWithParents(record_path, updated);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"token\":\"{s}\",\"status\":\"approved\"}}\n", .{token});
}

pub fn scopeCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "set")) return common.fail("usage: kelp-pi scope set --host HOST --until RFC3339", 64);
    const data_dir = common.option(args[1..], "--data-dir") orelse common.default_data_dir;
    const host = common.option(args[1..], "--host") orelse common.option(args[1..], "--url") orelse return common.fail("scope requires --host or --url", 64);
    const until = common.option(args[1..], "--until") orelse "manual";
    const scope_id = common.option(args[1..], "--scope-id") orelse "default";
    const scope_dir = try common.pathJoin(allocator, data_dir, "scope");
    defer allocator.free(scope_dir);
    try std.fs.cwd().makePath(scope_dir);
    const scope_path = try common.pathJoin(allocator, scope_dir, "current-scope.json");
    defer allocator.free(scope_path);
    const payload = try std.fmt.allocPrint(
        allocator,
        "{{\"schemaVersion\":\"kelp.pi.scope.v1\",\"scopeId\":\"{s}\",\"host\":\"{s}\",\"validUntil\":\"{s}\"}}\n",
        .{ scope_id, host, until },
    );
    defer allocator.free(payload);
    try common.writeFileWithParents(scope_path, payload);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"scopeId\":\"{s}\",\"host\":\"{s}\",\"path\":\"{s}\"}}\n", .{ scope_id, host, scope_path });
}

pub fn targetInScope(allocator: std.mem.Allocator, data_dir: []const u8, target: []const u8) bool {
    const scope_path = common.pathJoin3(allocator, data_dir, "scope", "current-scope.json") catch return false;
    defer allocator.free(scope_path);
    const content = std.fs.cwd().readFileAlloc(allocator, scope_path, 1024 * 1024) catch return false;
    defer allocator.free(content);
    return common.containsIgnoreCase(content, target) or common.containsIgnoreCase(target, common.extractJsonField(content, "host") orelse "");
}

fn approvalPath(allocator: std.mem.Allocator, data_dir: []const u8, token: []const u8) ![]u8 {
    const filename = try std.fmt.allocPrint(allocator, "{s}.json", .{token});
    defer allocator.free(filename);
    const approvals_dir = try common.pathJoin(allocator, data_dir, "approvals");
    defer allocator.free(approvals_dir);
    return common.pathJoin(allocator, approvals_dir, filename);
}

pub fn approvalApproved(allocator: std.mem.Allocator, data_dir: []const u8, token: []const u8) bool {
    const path = approvalPath(allocator, data_dir, token) catch return false;
    defer allocator.free(path);
    const content = std.fs.cwd().readFileAlloc(allocator, path, 1024 * 1024) catch return false;
    defer allocator.free(content);
    const now = std.time.timestamp();
    const expires = common.extractJsonInt(content, "expiresAtUnix") orelse return false;
    return expires >= now and common.containsIgnoreCase(content, "\"status\":\"approved\"");
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
