const std = @import("std");
const audit = @import("audit.zig");
const common = @import("common.zig");

pub fn outboxCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0) return common.fail("usage: kelp-pi outbox <enqueue|replay>", 64);
    if (std.mem.eql(u8, args[0], "enqueue")) return enqueueCommand(allocator, args[1..]);
    if (std.mem.eql(u8, args[0], "replay")) return replayCommand(allocator, args[1..]);
    return common.fail("usage: kelp-pi outbox <enqueue|replay>", 64);
}

fn enqueueCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const data_dir = common.option(args, "--data-dir") orelse common.default_data_dir;
    const kind = common.option(args, "--kind") orelse return common.fail("outbox enqueue requires --kind", 64);
    const msg_id = common.option(args, "--msg-id") orelse return common.fail("outbox enqueue requires --msg-id", 64);
    const payload = common.option(args, "--payload-json") orelse "{}";
    if (!common.safePathSegment(msg_id)) return common.fail("outbox msg id contains unsafe characters", 64);
    const path = try queuedPath(allocator, data_dir, msg_id);
    defer allocator.free(path);
    const envelope = try std.fmt.allocPrint(allocator, "{{\"schemaVersion\":\"kelp.pi.envelope.v1\",\"kind\":\"{s}\",\"msg_id\":\"{s}\",\"payload\":{s}}}\n", .{ kind, msg_id, payload });
    defer allocator.free(envelope);
    try common.writeFileWithParents(path, envelope);
    try audit.appendEvent(allocator, data_dir, "outbox.enqueued", msg_id, kind);
    const out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"msgId\":\"{s}\",\"path\":\"{s}\"}}\n", .{ msg_id, path });
}

fn replayCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const data_dir = common.option(args, "--data-dir") orelse common.default_data_dir;
    const out = std.fs.File.stdout().deprecatedWriter();
    try replayQueued(allocator, data_dir, out);
}

fn replayQueued(allocator: std.mem.Allocator, data_dir: []const u8, out: anytype) !void {
    const queue_dir_path = try common.pathJoin3(allocator, data_dir, "outbox", "queue");
    defer allocator.free(queue_dir_path);
    const sent_dir_path = try common.pathJoin3(allocator, data_dir, "outbox", "sent");
    defer allocator.free(sent_dir_path);
    std.fs.cwd().makePath(sent_dir_path) catch {};
    var names: std.ArrayList([]u8) = .empty;
    defer {
        for (names.items) |name| allocator.free(name);
        names.deinit(allocator);
    }
    var dir = std.fs.cwd().openDir(queue_dir_path, .{ .iterate = true }) catch {
        return;
    };
    defer dir.close();
    var iterator = dir.iterate();
    while (try iterator.next()) |entry| {
        if (entry.kind != .file) continue;
        try names.append(allocator, try allocator.dupe(u8, entry.name));
    }
    std.mem.sort([]u8, names.items, {}, stringLessThan);
    for (names.items) |name| {
        const queued = try common.pathJoin(allocator, queue_dir_path, name);
        defer allocator.free(queued);
        const sent = try common.pathJoin(allocator, sent_dir_path, name);
        defer allocator.free(sent);
        const content = try std.fs.cwd().readFileAlloc(allocator, queued, 1024 * 1024);
        defer allocator.free(content);
        try out.writeAll(content);
        try std.fs.cwd().rename(queued, sent);
        try audit.appendEvent(allocator, data_dir, "outbox.sent", name, sent);
    }
}

fn queuedPath(allocator: std.mem.Allocator, data_dir: []const u8, msg_id: []const u8) ![]u8 {
    const filename = try std.fmt.allocPrint(allocator, "{s}.json", .{msg_id});
    defer allocator.free(filename);
    const queue_dir = try common.pathJoin3(allocator, data_dir, "outbox", "queue");
    defer allocator.free(queue_dir);
    return common.pathJoin(allocator, queue_dir, filename);
}

fn stringLessThan(_: void, left: []u8, right: []u8) bool {
    return std.mem.lessThan(u8, left, right);
}

test "outbox replay emits queued envelopes in order and moves them to sent" {
    const root = try common.testTempPath(std.testing.allocator, "outbox-replay");
    defer std.testing.allocator.free(root);
    defer std.fs.cwd().deleteTree(root) catch {};
    try enqueueForTest(std.testing.allocator, root, "replay-003", "{\"seq\":3}");
    try enqueueForTest(std.testing.allocator, root, "replay-001", "{\"seq\":1}");
    try enqueueForTest(std.testing.allocator, root, "replay-002", "{\"seq\":2}");
    const queue_one = try queuedPath(std.testing.allocator, root, "replay-001");
    defer std.testing.allocator.free(queue_one);
    const sent_one = try common.pathJoin3(std.testing.allocator, root, "outbox/sent", "replay-001.json");
    defer std.testing.allocator.free(sent_one);
    var output: std.ArrayList(u8) = .empty;
    defer output.deinit(std.testing.allocator);
    var writer = output.writer(std.testing.allocator);
    try replayQueued(std.testing.allocator, root, writer);
    try std.testing.expect(!common.fileExists(queue_one));
    try std.testing.expect(common.fileExists(sent_one));
    try std.testing.expect(std.mem.indexOf(u8, output.items, "\"msg_id\":\"replay-001\"") != null);
    try std.testing.expect(std.mem.indexOf(u8, output.items, "\"msg_id\":\"replay-002\"") != null);
    try std.testing.expect(std.mem.indexOf(u8, output.items, "\"msg_id\":\"replay-003\"") != null);
    output.clearRetainingCapacity();
    writer = output.writer(std.testing.allocator);
    try replayQueued(std.testing.allocator, root, writer);
    try std.testing.expectEqual(@as(usize, 0), output.items.len);
}

fn enqueueForTest(allocator: std.mem.Allocator, data_dir: []const u8, msg_id: []const u8, payload: []const u8) !void {
    const path = try queuedPath(allocator, data_dir, msg_id);
    defer allocator.free(path);
    const envelope = try std.fmt.allocPrint(allocator, "{{\"kind\":\"evidence.append\",\"msg_id\":\"{s}\",\"payload\":{s}}}\n", .{ msg_id, payload });
    defer allocator.free(envelope);
    try common.writeFileWithParents(path, envelope);
}
