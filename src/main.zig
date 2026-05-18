const std = @import("std");
const cli = @import("cli.zig");

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    var arena = std.heap.ArenaAllocator.init(gpa.allocator());
    defer arena.deinit();
    const allocator = arena.allocator();
    const args = try std.process.argsAlloc(allocator);
    const output = try cli.run(allocator, args);
    if (output.stdout.len > 0) try std.fs.File.stdout().writeAll(output.stdout);
    if (output.stderr.len > 0) try std.fs.File.stderr().writeAll(output.stderr);
    std.process.exit(@intCast(output.exit_code));
}
