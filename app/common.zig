const std = @import("std");

pub const default_data_dir = "/var/lib/kelp-pi";
pub const default_policy_path = "policies/appsec-agent-baseline.toml";
pub const default_model_manifest = "models/manifest.toml";
pub const default_no_answer_threshold = 0.000001;

pub fn option(args: []const []const u8, name: []const u8) ?[]const u8 {
    var index: usize = 0;
    while (index + 1 < args.len) : (index += 1) {
        if (std.mem.eql(u8, args[index], name)) return args[index + 1];
    }
    return null;
}

pub fn hasFlag(args: []const []const u8, name: []const u8) bool {
    for (args) |arg| if (std.mem.eql(u8, arg, name)) return true;
    return false;
}

pub fn firstPositional(args: []const []const u8, skip_opt: []const u8) ?[]const u8 {
    var index: usize = 0;
    while (index < args.len) : (index += 1) {
        if (std.mem.eql(u8, args[index], skip_opt)) {
            index += 1;
            continue;
        }
        if (!startsWith(args[index], "--")) return args[index];
    }
    return null;
}

pub fn argsAfterDoubleDash(args: []const []const u8) []const []const u8 {
    for (args, 0..) |arg, index| {
        if (std.mem.eql(u8, arg, "--")) return args[index + 1 ..];
    }
    return &.{};
}

pub fn attr(line: []const u8, name: []const u8) ?[]const u8 {
    var needle_buf: [64]u8 = undefined;
    const needle = std.fmt.bufPrint(&needle_buf, "{s}=\"", .{name}) catch return null;
    const start = std.mem.indexOf(u8, line, needle) orelse return null;
    const after = line[start + needle.len ..];
    const end = std.mem.indexOfScalar(u8, after, '"') orelse return null;
    return after[0..end];
}

pub fn extractJsonField(text: []const u8, field: []const u8) ?[]const u8 {
    var needle_buf: [128]u8 = undefined;
    const needle = std.fmt.bufPrint(&needle_buf, "\"{s}\":\"", .{field}) catch return null;
    const start = std.mem.indexOf(u8, text, needle) orelse return null;
    const after = text[start + needle.len ..];
    const end = std.mem.indexOfScalar(u8, after, '"') orelse return null;
    return after[0..end];
}

pub fn extractJsonInt(text: []const u8, field: []const u8) ?i64 {
    var needle_buf: [128]u8 = undefined;
    const needle = std.fmt.bufPrint(&needle_buf, "\"{s}\":", .{field}) catch return null;
    const start = std.mem.indexOf(u8, text, needle) orelse return null;
    const after = std.mem.trimLeft(u8, text[start + needle.len ..], " \t\r\n");
    var end: usize = 0;
    while (end < after.len and (std.ascii.isDigit(after[end]) or after[end] == '-')) : (end += 1) {}
    if (end == 0) return null;
    return std.fmt.parseInt(i64, after[0..end], 10) catch null;
}

pub fn printJsonStatus(ok: bool, action: []const u8, reason: []const u8) !void {
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":{},\"action\":\"{s}\",\"reason\":\"{s}\"}}\n", .{ ok, action, reason });
}

pub fn printLine(line: []const u8) !void {
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{s}\n", .{line});
}

pub fn fail(message: []const u8, code: u8) !void {
    std.debug.print("{s}\n", .{message});
    std.process.exit(code);
}

pub fn fileExists(path: []const u8) bool {
    std.fs.cwd().access(path, .{}) catch return false;
    return true;
}

pub fn writeFileWithParents(path: []const u8, content: []const u8) !void {
    if (std.fs.path.dirname(path)) |parent| try std.fs.cwd().makePath(parent);
    var file = try std.fs.cwd().createFile(path, .{ .truncate = true });
    defer file.close();
    var writer = file.deprecatedWriter();
    try writer.writeAll(content);
}

pub fn pathJoin(allocator: std.mem.Allocator, left: []const u8, right: []const u8) ![]u8 {
    return std.fmt.allocPrint(allocator, "{s}/{s}", .{ left, right });
}

pub fn pathJoin3(allocator: std.mem.Allocator, a: []const u8, b: []const u8, c: []const u8) ![]u8 {
    return std.fmt.allocPrint(allocator, "{s}/{s}/{s}", .{ a, b, c });
}

pub fn safePathSegment(value: []const u8) bool {
    if (value.len == 0 or value.len > 128) return false;
    for (value) |byte| {
        if (!(std.ascii.isAlphanumeric(byte) or byte == '-' or byte == '_' or byte == '.')) return false;
    }
    return true;
}

pub fn contentHashHex(allocator: std.mem.Allocator, content: []const u8) ![]u8 {
    var digest: [32]u8 = undefined;
    std.crypto.hash.sha2.Sha256.hash(content, &digest, .{});
    return hexAlloc(allocator, &digest);
}

pub fn fileHashHex(allocator: std.mem.Allocator, path: []const u8) ![]u8 {
    var file = try std.fs.cwd().openFile(path, .{});
    defer file.close();
    var hasher = std.crypto.hash.sha2.Sha256.init(.{});
    var buffer: [65536]u8 = undefined;
    while (true) {
        const read = try file.read(&buffer);
        if (read == 0) break;
        hasher.update(buffer[0..read]);
    }
    var digest: [32]u8 = undefined;
    hasher.final(&digest);
    return hexAlloc(allocator, &digest);
}

pub fn hexAlloc(allocator: std.mem.Allocator, bytes: []const u8) ![]u8 {
    const out = try allocator.alloc(u8, bytes.len * 2);
    const alphabet = "0123456789abcdef";
    for (bytes, 0..) |byte, index| {
        out[index * 2] = alphabet[byte >> 4];
        out[index * 2 + 1] = alphabet[byte & 0x0f];
    }
    return out;
}

pub fn hexToBytes(hex: []const u8, out: []u8) !void {
    if (hex.len != out.len * 2) return error.InvalidHex;
    for (out, 0..) |*byte, index| {
        byte.* = (try hexNibble(hex[index * 2]) << 4) | try hexNibble(hex[index * 2 + 1]);
    }
}

pub fn hexNibble(byte: u8) !u8 {
    if (byte >= '0' and byte <= '9') return byte - '0';
    if (byte >= 'a' and byte <= 'f') return byte - 'a' + 10;
    if (byte >= 'A' and byte <= 'F') return byte - 'A' + 10;
    return error.InvalidHex;
}

pub fn writeJsonEscaped(writer: anytype, text: []const u8) !void {
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

pub fn startsWith(value: []const u8, prefix: []const u8) bool {
    return std.mem.startsWith(u8, value, prefix);
}

pub fn containsIgnoreCase(haystack: []const u8, needle: []const u8) bool {
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

pub fn parseUsize(value: []const u8, fallback: usize) usize {
    return std.fmt.parseInt(usize, value, 10) catch fallback;
}

pub fn parseU64AfterEquals(line: []const u8, fallback: u64) u64 {
    const eq = std.mem.indexOfScalar(u8, line, '=') orelse return fallback;
    return std.fmt.parseInt(u64, std.mem.trim(u8, line[eq + 1 ..], " \t\r"), 10) catch fallback;
}

pub fn quotedValue(line: []const u8) ?[]const u8 {
    const first = std.mem.indexOfScalar(u8, line, '"') orelse return null;
    const after = line[first + 1 ..];
    const second = std.mem.indexOfScalar(u8, after, '"') orelse return null;
    return after[0..second];
}

pub fn fileHasMagic(path: []const u8, magic: []const u8) !bool {
    var file = try std.fs.cwd().openFile(path, .{});
    defer file.close();
    var buffer: [8]u8 = undefined;
    const read = try file.read(&buffer);
    return read >= magic.len and std.mem.eql(u8, buffer[0..magic.len], magic);
}

pub fn chmod600(path: []const u8) void {
    if (@import("builtin").os.tag == .windows) return;
    var file = std.fs.cwd().openFile(path, .{}) catch return;
    defer file.close();
    file.chmod(0o600) catch {};
}

pub fn testTempPath(allocator: std.mem.Allocator, prefix: []const u8) ![]u8 {
    var random_bytes: [8]u8 = undefined;
    std.crypto.random.bytes(&random_bytes);
    const suffix = try hexAlloc(allocator, &random_bytes);
    defer allocator.free(suffix);
    return std.fmt.allocPrint(allocator, ".zig-cache/{s}-{s}", .{ prefix, suffix });
}
