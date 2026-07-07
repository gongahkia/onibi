const std = @import("std");
const audit = @import("audit.zig");
const common = @import("common.zig");

pub const Scope = enum {
    scan,
    ingest,
    upload,

    pub fn text(self: Scope) []const u8 {
        return switch (self) {
            .scan => "scan",
            .ingest => "ingest",
            .upload => "upload",
        };
    }
};

pub const Check = struct {
    scope: Scope,
    min_free_bytes: u64,
    available_bytes: u64,
};

pub const QuotaError = error{ StorageQuotaExceeded, InvalidMinFreeBytes };

pub fn evaluateStorageQuota(scope: Scope, min_free_bytes: u64, available_bytes: u64) QuotaError!Check {
    if (min_free_bytes == 0) return error.InvalidMinFreeBytes;
    if (available_bytes < min_free_bytes) return error.StorageQuotaExceeded;
    return .{ .scope = scope, .min_free_bytes = min_free_bytes, .available_bytes = available_bytes };
}

pub fn enforceArgs(allocator: std.mem.Allocator, data_dir: []const u8, args: []const []const u8, scope: Scope) !void {
    const min_raw = common.option(args, "--min-free-bytes") orelse return;
    const min_free = std.fmt.parseInt(u64, min_raw, 10) catch return common.fail("invalid --min-free-bytes", 64);
    const available = if (common.option(args, "--available-bytes")) |raw|
        std.fmt.parseInt(u64, raw, 10) catch return common.fail("invalid --available-bytes", 64)
    else
        std.math.maxInt(u64);
    _ = evaluateStorageQuota(scope, min_free, available) catch |err| {
        const detail = try std.fmt.allocPrint(allocator, "scope={s}; min_free_bytes={}; available_bytes={}", .{ scope.text(), min_free, available });
        defer allocator.free(detail);
        try audit.appendEvent(allocator, data_dir, "storage.quota.refused", scope.text(), detail);
        return err;
    };
}

test "storage quota refuses below floor and accepts sufficient space" {
    try std.testing.expectError(error.StorageQuotaExceeded, evaluateStorageQuota(.scan, 2048, 1024));
    const check = try evaluateStorageQuota(.upload, 1024, 2048);
    try std.testing.expectEqual(Scope.upload, check.scope);
    try std.testing.expectEqual(@as(u64, 1024), check.min_free_bytes);
}
