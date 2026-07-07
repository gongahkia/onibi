const std = @import("std");
const audit = @import("audit.zig");
const common = @import("common.zig");

pub fn chatCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const data_dir = common.option(args, "--data-dir") orelse common.default_data_dir;
    const session_id = common.option(args, "--session-id") orelse common.option(args, "--session") orelse "local";
    if (!common.safePathSegment(session_id)) return common.fail("chat session id contains unsafe characters", 64);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("kelp-pi chat session={s} data-dir={s}\n", .{ session_id, data_dir });
    try out.print("> ", .{});
    var stdin = std.fs.File.stdin().deprecatedReader();
    var buffer: [4096]u8 = undefined;
    while (try stdin.readUntilDelimiterOrEof(&buffer, '\n')) |line_raw| {
        const line = std.mem.trim(u8, line_raw, " \t\r\n");
        if (line.len == 0) {
            try out.print("> ", .{});
            continue;
        }
        try appendTranscriptRecord(allocator, data_dir, session_id, "user", line);
        if (std.mem.eql(u8, line, "exit") or std.mem.indexOf(u8, line, "<finish") != null) {
            try appendTranscriptRecord(allocator, data_dir, session_id, "assistant", "{\"ok\":true,\"finished\":true}");
            try out.print("{{\"ok\":true,\"finished\":true}}\n", .{});
            break;
        }
        if (common.attr(line, "query")) |query| {
            try appendTranscriptRecord(allocator, data_dir, session_id, "assistant", "tool=ask");
            try out.print("{{\"tool\":\"ask\",\"query\":\"{s}\"}}\n", .{query});
        } else if (common.attr(line, "target")) |target| {
            try appendTranscriptRecord(allocator, data_dir, session_id, "assistant", "tool=scan policy=require-approval");
            try out.print("{{\"tool\":\"scan\",\"target\":\"{s}\",\"policy\":\"require-approval\"}}\n", .{target});
        } else {
            try appendTranscriptRecord(allocator, data_dir, session_id, "assistant", line);
            try out.print("{{\"ok\":true,\"echo\":\"", .{});
            try common.writeJsonEscaped(&out, line);
            try out.print("\"}}\n", .{});
        }
        try out.print("> ", .{});
    }
}

pub fn appendTranscriptRecord(allocator: std.mem.Allocator, data_dir: []const u8, session_id: []const u8, role: []const u8, content: []const u8) !void {
    if (!common.safePathSegment(session_id)) return error.UnsafePathSegment;
    const session_dir = try common.pathJoin3(allocator, data_dir, "sessions", session_id);
    defer allocator.free(session_dir);
    try std.fs.cwd().makePath(session_dir);
    const transcript_path = try common.pathJoin(allocator, session_dir, "transcript.jsonl");
    defer allocator.free(transcript_path);
    var file = try std.fs.cwd().createFile(transcript_path, .{ .truncate = false });
    defer file.close();
    try file.seekFromEnd(0);
    var writer = file.deprecatedWriter();
    try writer.writeAll("{\"schemaVersion\":\"kelp.pi.transcript.v1\",\"sessionId\":\"");
    try common.writeJsonEscaped(&writer, session_id);
    try writer.print("\",\"tsUnix\":{},\"role\":\"{s}\",\"content\":\"", .{ std.time.timestamp(), role });
    try common.writeJsonEscaped(&writer, content);
    try writer.writeAll("\"}\n");
    try audit.appendEvent(allocator, data_dir, "transcript.write", session_id, role);
}

pub fn transcriptFilePath(allocator: std.mem.Allocator, data_dir: []const u8, session_id: []const u8) ![]u8 {
    if (!common.safePathSegment(session_id)) return error.UnsafePathSegment;
    const session_dir = try common.pathJoin3(allocator, data_dir, "sessions", session_id);
    defer allocator.free(session_dir);
    return common.pathJoin(allocator, session_dir, "transcript.jsonl");
}

test "transcript records append across turns" {
    const root = try common.testTempPath(std.testing.allocator, "transcript-append");
    defer std.testing.allocator.free(root);
    defer std.fs.cwd().deleteTree(root) catch {};
    try appendTranscriptRecord(std.testing.allocator, root, "session-a", "user", "hello");
    const transcript_path = try transcriptFilePath(std.testing.allocator, root, "session-a");
    defer std.testing.allocator.free(transcript_path);
    const first = try std.fs.cwd().readFileAlloc(std.testing.allocator, transcript_path, 64 * 1024);
    defer std.testing.allocator.free(first);
    try appendTranscriptRecord(std.testing.allocator, root, "session-a", "assistant", "world");
    const second = try std.fs.cwd().readFileAlloc(std.testing.allocator, transcript_path, 64 * 1024);
    defer std.testing.allocator.free(second);
    try std.testing.expect(second.len > first.len);
    try std.testing.expectEqual(@as(usize, 2), std.mem.count(u8, second, "\n"));
    try std.testing.expect(std.mem.indexOf(u8, second, "\"role\":\"user\"") != null);
    try std.testing.expect(std.mem.indexOf(u8, second, "\"role\":\"assistant\"") != null);
}
