const std = @import("std");
const bundle = @import("bundle.zig");
const chat = @import("chat.zig");
const common = @import("common.zig");
const doctor_mod = @import("doctor.zig");
const index = @import("index.zig");
const keys = @import("keys.zig");
const model = @import("model.zig");
const policy = @import("policy.zig");
const scanner = @import("scanner.zig");
const scope = @import("scope.zig");

pub fn main() !void {
    var gpa = std.heap.GeneralPurposeAllocator(.{}){};
    defer _ = gpa.deinit();
    const allocator = gpa.allocator();
    const args = try std.process.argsAlloc(allocator);
    defer std.process.argsFree(allocator, args);

    if (args.len <= 1) return usage();
    const command = args[1];
    if (std.mem.eql(u8, command, "version") or std.mem.eql(u8, command, "--version")) return common.printLine("kelp-pi 0.2.0-zig");
    if (std.mem.eql(u8, command, "doctor")) return doctor_mod.doctor(args[2..]);
    if (std.mem.eql(u8, command, "keygen")) return keys.keygen(allocator, args[2..]);
    if (std.mem.eql(u8, command, "policy")) return policy.policyCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "approval-request")) return scope.approvalRequest(allocator, args[2..]);
    if (std.mem.eql(u8, command, "approve")) return scope.approve(allocator, args[2..]);
    if (std.mem.eql(u8, command, "scope")) return scope.scopeCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "scan")) return scanner.scanCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "index")) return index.indexCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "ask")) return index.askCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "bundle")) return bundle.bundleCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "verify-bundle")) return bundle.verifyBundle(allocator, args[2..]);
    if (std.mem.eql(u8, command, "model")) return model.modelCommand(allocator, args[2..]);
    if (std.mem.eql(u8, command, "chat")) return chat.chatCommand(allocator, args[2..]);
    return common.fail("unknown command", 64);
}

fn usage() !void {
    return common.printLine("usage: kelp-pi <chat|doctor|keygen|policy|approval-request|approve|scope|scan|index|ask|bundle|verify-bundle|model|version>");
}

test {
    std.testing.refAllDecls(@import("bundle.zig"));
    std.testing.refAllDecls(@import("chat.zig"));
    std.testing.refAllDecls(@import("doctor.zig"));
    std.testing.refAllDecls(@import("index.zig"));
    std.testing.refAllDecls(@import("keys.zig"));
    std.testing.refAllDecls(@import("model.zig"));
    std.testing.refAllDecls(@import("policy.zig"));
    std.testing.refAllDecls(@import("scanner.zig"));
    std.testing.refAllDecls(@import("scope.zig"));
}
