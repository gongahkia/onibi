const std = @import("std");
const cli = @import("cli.zig");
const dates = @import("dates.zig");
const domain = @import("domain.zig");
const storage = @import("storage.zig");

test "date and recurrence behavior" {
    _ = dates;
    _ = domain;
}

test "json task workflow through cli" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    const root = try std.fs.path.join(allocator, &.{ "/tmp", try std.fmt.allocPrint(allocator, "kelp-zig-test-{d}", .{std.time.nanoTimestamp()}) });
    const add = try cli.run(allocator, &.{ "kelp", "--data-dir", root, "--output", "json", "task", "add", "--title", "Ready task", "--due", "2026-03-20" });
    try std.testing.expectEqual(@as(i32, 0), add.exit_code);
    try std.testing.expect(std.mem.indexOf(u8, add.stdout, "\"command\": \"task.add\"") != null);
    const list = try cli.run(allocator, &.{ "kelp", "--data-dir", root, "--output", "json", "task", "list" });
    try std.testing.expect(std.mem.indexOf(u8, list.stdout, "Ready task") != null);
}

test "storage serializes schema 5" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    const allocator = arena.allocator();
    var state = domain.AppState.init(allocator);
    _ = try state.createTask(.{ .title = "Persist me" }, "2026-03-14");
    const data = try storage.renderStateJson(allocator, &state);
    try std.testing.expect(std.mem.indexOf(u8, data, "\"schema_version\": 5") != null);
}
