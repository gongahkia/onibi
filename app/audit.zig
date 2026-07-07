const std = @import("std");
const common = @import("common.zig");

const zero_hash = "0000000000000000000000000000000000000000000000000000000000000000";

const VerifyResult = struct {
    entries: usize,
    head_hash: []u8,
};

pub fn auditCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "verify")) return common.fail("usage: kelp-pi audit verify [--data-dir DIR] [--log-file PATH]", 64);
    return verifyCommand(allocator, args[1..]);
}

pub fn verifyAuditLogCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    return verifyCommand(allocator, args);
}

fn verifyCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const data_dir = common.option(args, "--data-dir") orelse common.default_data_dir;
    var log_alloc: ?[]u8 = null;
    defer if (log_alloc) |path| allocator.free(path);
    const log_path = common.option(args, "--log-file") orelse blk: {
        log_alloc = try auditLogPath(allocator, data_dir);
        break :blk log_alloc.?;
    };
    var head_alloc: ?[]u8 = null;
    defer if (head_alloc) |path| allocator.free(path);
    const head_path = common.option(args, "--head-file") orelse blk: {
        head_alloc = try auditHeadPath(allocator, data_dir);
        break :blk head_alloc.?;
    };
    const result = verifyAuditLog(allocator, log_path, head_path) catch |err| {
        var out = std.fs.File.stdout().deprecatedWriter();
        return out.print("{{\"ok\":false,\"logFile\":\"{s}\",\"reason\":\"{s}\"}}\n", .{ log_path, @errorName(err) });
    };
    defer allocator.free(result.head_hash);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"logFile\":\"{s}\",\"entries\":{},\"headHash\":\"{s}\"}}\n", .{ log_path, result.entries, result.head_hash });
}

pub fn appendEvent(allocator: std.mem.Allocator, data_dir: []const u8, event: []const u8, subject: []const u8, detail: []const u8) !void {
    const log_path = try auditLogPath(allocator, data_dir);
    defer allocator.free(log_path);
    const head_path = try auditHeadPath(allocator, data_dir);
    defer allocator.free(head_path);
    if (std.fs.path.dirname(log_path)) |parent| try std.fs.cwd().makePath(parent);
    if (common.fileExists(log_path) and common.fileExists(head_path)) {
        const verified = try verifyAuditLog(allocator, log_path, head_path);
        defer allocator.free(verified.head_hash);
    }

    const previous = try lastState(allocator, log_path);
    defer allocator.free(previous.head_hash);
    const seq = previous.entries + 1;
    const ts = std.time.timestamp();
    const current = try eventHash(allocator, seq, ts, event, subject, detail, previous.head_hash);
    defer allocator.free(current);

    var file = try std.fs.cwd().createFile(log_path, .{ .truncate = false });
    defer file.close();
    try file.seekFromEnd(0);
    var writer = file.deprecatedWriter();
    try writer.print("{{\"schemaVersion\":\"kelp.pi.audit.v1\",\"seq\":{},\"tsUnix\":{},\"event\":\"", .{ seq, ts });
    try common.writeJsonEscaped(&writer, event);
    try writer.writeAll("\",\"subject\":\"");
    try common.writeJsonEscaped(&writer, subject);
    try writer.writeAll("\",\"detail\":\"");
    try common.writeJsonEscaped(&writer, detail);
    try writer.writeAll("\",\"previousHash\":\"");
    try writer.writeAll(previous.head_hash);
    try writer.writeAll("\",\"currentHash\":\"");
    try writer.writeAll(current);
    try writer.writeAll("\"}\n");
    try writeHead(allocator, head_path, seq, current);
}

pub fn verifyAuditLog(allocator: std.mem.Allocator, log_path: []const u8, head_path: []const u8) !VerifyResult {
    if (!common.fileExists(log_path)) {
        if (common.fileExists(head_path)) return error.AuditHeadWithoutLog;
        return .{ .entries = 0, .head_hash = try allocator.dupe(u8, zero_hash) };
    }
    const content = try std.fs.cwd().readFileAlloc(allocator, log_path, 16 * 1024 * 1024);
    defer allocator.free(content);
    var expected_previous: []const u8 = zero_hash;
    var expected_seq: usize = 1;
    var lines = std.mem.splitScalar(u8, content, '\n');
    var head_hash: []const u8 = zero_hash;
    while (lines.next()) |raw_line| {
        const line = std.mem.trim(u8, raw_line, " \t\r");
        if (line.len == 0) continue;
        const seq_raw = common.extractJsonInt(line, "seq") orelse return error.MissingSeq;
        if (seq_raw < 1) return error.InvalidSeq;
        const seq: usize = @intCast(seq_raw);
        if (seq != expected_seq) return error.AuditSequenceMismatch;
        const ts = common.extractJsonInt(line, "tsUnix") orelse return error.MissingTimestamp;
        const event = try auditJsonField(allocator, line, "event");
        defer allocator.free(event);
        const subject = try auditJsonField(allocator, line, "subject");
        defer allocator.free(subject);
        const detail = try auditJsonField(allocator, line, "detail");
        defer allocator.free(detail);
        const previous = common.extractJsonField(line, "previousHash") orelse return error.MissingPreviousHash;
        const current = common.extractJsonField(line, "currentHash") orelse return error.MissingCurrentHash;
        if (!std.mem.eql(u8, previous, expected_previous)) return error.AuditPreviousHashMismatch;
        const recomputed = try eventHash(allocator, seq, ts, event, subject, detail, previous);
        defer allocator.free(recomputed);
        if (!std.ascii.eqlIgnoreCase(recomputed, current)) return error.AuditCurrentHashMismatch;
        expected_previous = current;
        head_hash = current;
        expected_seq += 1;
    }
    const entries = expected_seq - 1;
    if (common.fileExists(head_path)) try verifyHead(allocator, head_path, entries, head_hash);
    return .{ .entries = entries, .head_hash = try allocator.dupe(u8, head_hash) };
}

pub fn auditLogPath(allocator: std.mem.Allocator, data_dir: []const u8) ![]u8 {
    return common.pathJoin3(allocator, data_dir, "audit", "agent.jsonl");
}

pub fn auditHeadPath(allocator: std.mem.Allocator, data_dir: []const u8) ![]u8 {
    return common.pathJoin3(allocator, data_dir, "audit", "head.json");
}

fn lastState(allocator: std.mem.Allocator, log_path: []const u8) !VerifyResult {
    if (!common.fileExists(log_path)) return .{ .entries = 0, .head_hash = try allocator.dupe(u8, zero_hash) };
    const content = try std.fs.cwd().readFileAlloc(allocator, log_path, 16 * 1024 * 1024);
    defer allocator.free(content);
    var entries: usize = 0;
    var head_hash: []const u8 = zero_hash;
    var lines = std.mem.splitScalar(u8, content, '\n');
    while (lines.next()) |raw_line| {
        const line = std.mem.trim(u8, raw_line, " \t\r");
        if (line.len == 0) continue;
        const seq_raw = common.extractJsonInt(line, "seq") orelse return error.MissingSeq;
        if (seq_raw < 1) return error.InvalidSeq;
        entries = @intCast(seq_raw);
        head_hash = common.extractJsonField(line, "currentHash") orelse return error.MissingCurrentHash;
    }
    return .{ .entries = entries, .head_hash = try allocator.dupe(u8, head_hash) };
}

fn eventHash(allocator: std.mem.Allocator, seq: usize, ts: i64, event: []const u8, subject: []const u8, detail: []const u8, previous_hash: []const u8) ![]u8 {
    const material = try std.fmt.allocPrint(allocator, "{d}\n{d}\n{s}\n{s}\n{s}\n{s}\n", .{ seq, ts, event, subject, detail, previous_hash });
    defer allocator.free(material);
    return common.contentHashHex(allocator, material);
}

fn writeHead(allocator: std.mem.Allocator, head_path: []const u8, entries: usize, head_hash: []const u8) !void {
    const payload = try std.fmt.allocPrint(allocator, "{{\"schemaVersion\":\"kelp.pi.audit-head.v1\",\"entries\":{},\"headHash\":\"{s}\"}}\n", .{ entries, head_hash });
    defer allocator.free(payload);
    try common.writeFileWithParents(head_path, payload);
}

fn verifyHead(allocator: std.mem.Allocator, head_path: []const u8, entries: usize, head_hash: []const u8) !void {
    const content = try std.fs.cwd().readFileAlloc(allocator, head_path, 4096);
    defer allocator.free(content);
    const expected_entries = common.extractJsonInt(content, "entries") orelse return error.MissingHeadEntries;
    const expected_hash = common.extractJsonField(content, "headHash") orelse return error.MissingHeadHash;
    if (expected_entries != @as(i64, @intCast(entries))) return error.AuditHeadEntriesMismatch;
    if (!std.ascii.eqlIgnoreCase(expected_hash, head_hash)) return error.AuditHeadHashMismatch;
}

fn auditJsonField(allocator: std.mem.Allocator, text: []const u8, field: []const u8) ![]u8 {
    var needle_buf: [128]u8 = undefined;
    const needle = std.fmt.bufPrint(&needle_buf, "\"{s}\":\"", .{field}) catch return error.MissingAuditField;
    const start = std.mem.indexOf(u8, text, needle) orelse return error.MissingAuditField;
    var index = start + needle.len;
    var out: std.ArrayList(u8) = .empty;
    defer out.deinit(allocator);
    while (index < text.len) : (index += 1) {
        const byte = text[index];
        if (byte == '"') return out.toOwnedSlice(allocator);
        if (byte == '\\') {
            index += 1;
            if (index >= text.len) return error.BadAuditJsonString;
            switch (text[index]) {
                '"' => try out.append(allocator, '"'),
                '\\' => try out.append(allocator, '\\'),
                'n' => try out.append(allocator, '\n'),
                'r' => try out.append(allocator, '\r'),
                't' => try out.append(allocator, '\t'),
                else => return error.BadAuditJsonString,
            }
            continue;
        }
        try out.append(allocator, byte);
    }
    return error.BadAuditJsonString;
}

test "audit verifier detects mutation deletion and insertion" {
    const root = try common.testTempPath(std.testing.allocator, "audit-chain");
    defer std.testing.allocator.free(root);
    defer std.fs.cwd().deleteTree(root) catch {};
    const log_path = try auditLogPath(std.testing.allocator, root);
    defer std.testing.allocator.free(log_path);
    const head_path = try auditHeadPath(std.testing.allocator, root);
    defer std.testing.allocator.free(head_path);

    try appendEvent(std.testing.allocator, root, "policy.decision", "Bash", "allow");
    try appendEvent(std.testing.allocator, root, "approval.requested", "token", "pending");
    try appendEvent(std.testing.allocator, root, "scan.completed", "nuclei", "ok");
    const ok = try verifyAuditLog(std.testing.allocator, log_path, head_path);
    defer std.testing.allocator.free(ok.head_hash);
    try std.testing.expectEqual(@as(usize, 3), ok.entries);

    const original = try std.fs.cwd().readFileAlloc(std.testing.allocator, log_path, 64 * 1024);
    defer std.testing.allocator.free(original);
    const mutated = try replaceOnce(std.testing.allocator, original, "scan.completed", "scan.tampered");
    defer std.testing.allocator.free(mutated);
    try common.writeFileWithParents(log_path, mutated);
    try std.testing.expectError(error.AuditCurrentHashMismatch, verifyAuditLog(std.testing.allocator, log_path, head_path));

    try common.writeFileWithParents(log_path, original);
    const deleted = try removeLineContaining(std.testing.allocator, original, "approval.requested");
    defer std.testing.allocator.free(deleted);
    try common.writeFileWithParents(log_path, deleted);
    try std.testing.expectError(error.AuditSequenceMismatch, verifyAuditLog(std.testing.allocator, log_path, head_path));

    try common.writeFileWithParents(log_path, original);
    const inserted = try insertDuplicateFirstLine(std.testing.allocator, original);
    defer std.testing.allocator.free(inserted);
    try common.writeFileWithParents(log_path, inserted);
    try std.testing.expectError(error.AuditSequenceMismatch, verifyAuditLog(std.testing.allocator, log_path, head_path));

    try common.writeFileWithParents(log_path, original);
    const tail_deleted = try removeLineContaining(std.testing.allocator, original, "scan.completed");
    defer std.testing.allocator.free(tail_deleted);
    try common.writeFileWithParents(log_path, tail_deleted);
    try std.testing.expectError(error.AuditHeadEntriesMismatch, verifyAuditLog(std.testing.allocator, log_path, head_path));
}

fn replaceOnce(allocator: std.mem.Allocator, content: []const u8, needle: []const u8, replacement: []const u8) ![]u8 {
    const start = std.mem.indexOf(u8, content, needle) orelse return error.NotFound;
    var out: std.ArrayList(u8) = .empty;
    defer out.deinit(allocator);
    try out.appendSlice(allocator, content[0..start]);
    try out.appendSlice(allocator, replacement);
    try out.appendSlice(allocator, content[start + needle.len ..]);
    return out.toOwnedSlice(allocator);
}

fn removeLineContaining(allocator: std.mem.Allocator, content: []const u8, needle: []const u8) ![]u8 {
    var out: std.ArrayList(u8) = .empty;
    defer out.deinit(allocator);
    var lines = std.mem.splitScalar(u8, content, '\n');
    while (lines.next()) |line| {
        if (line.len == 0) continue;
        if (std.mem.indexOf(u8, line, needle) != null) continue;
        try out.appendSlice(allocator, line);
        try out.append(allocator, '\n');
    }
    return out.toOwnedSlice(allocator);
}

fn insertDuplicateFirstLine(allocator: std.mem.Allocator, content: []const u8) ![]u8 {
    const end = std.mem.indexOfScalar(u8, content, '\n') orelse return error.NotFound;
    var out: std.ArrayList(u8) = .empty;
    defer out.deinit(allocator);
    try out.appendSlice(allocator, content[0 .. end + 1]);
    try out.appendSlice(allocator, content);
    return out.toOwnedSlice(allocator);
}
