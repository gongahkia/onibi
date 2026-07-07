const std = @import("std");
const common = @import("common.zig");
const keys = @import("keys.zig");

pub fn acceptanceCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0) return common.fail("usage: kelp-pi acceptance <sign|verify> --artifact-dir DIR", 64);
    if (std.mem.eql(u8, args[0], "sign")) return signCommand(allocator, args[1..]);
    if (std.mem.eql(u8, args[0], "verify")) return verifyCommand(allocator, args[1..]);
    return common.fail("usage: kelp-pi acceptance <sign|verify> --artifact-dir DIR", 64);
}

fn signCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const artifact_dir = common.option(args, "--artifact-dir") orelse return common.fail("acceptance sign requires --artifact-dir", 64);
    const data_dir = common.option(args, "--data-dir") orelse common.default_data_dir;
    const result = try signAcceptanceManifest(allocator, data_dir, artifact_dir);
    defer allocator.free(result.manifest);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"artifactDir\":\"{s}\",\"fileCount\":{}}}\n", .{ artifact_dir, result.file_count });
}

fn verifyCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const artifact_dir = common.option(args, "--artifact-dir") orelse return common.fail("acceptance verify requires --artifact-dir", 64);
    const ok = verifyAcceptanceManifest(allocator, artifact_dir) catch false;
    if (!ok) return common.fail("acceptance manifest verification failed", 77);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"artifactDir\":\"{s}\"}}\n", .{artifact_dir});
}

const SignResult = struct {
    manifest: []u8,
    file_count: usize,
};

pub fn signAcceptanceManifest(allocator: std.mem.Allocator, data_dir: []const u8, artifact_dir: []const u8) !SignResult {
    const key_dir = try common.pathJoin(allocator, data_dir, "keys");
    defer allocator.free(key_dir);
    var key = try keys.loadOrCreateKey(allocator, key_dir);
    defer keys.freeKeyMaterial(allocator, &key);
    const manifest = try buildManifest(allocator, artifact_dir, key.public_hex[0..16]);
    errdefer allocator.free(manifest);
    const manifest_path = try common.pathJoin(allocator, artifact_dir, "acceptance-manifest.json");
    defer allocator.free(manifest_path);
    try common.writeFileWithParents(manifest_path, manifest);
    const sig = try key.key_pair.sign(manifest, null);
    const sig_bytes = sig.toBytes();
    const sig_hex = try common.hexAlloc(allocator, &sig_bytes);
    defer allocator.free(sig_hex);
    const sig_path = try common.pathJoin(allocator, artifact_dir, "acceptance-manifest.sig");
    defer allocator.free(sig_path);
    try common.writeFileWithParents(sig_path, sig_hex);
    const pub_path = try common.pathJoin(allocator, artifact_dir, "acceptance-manifest.pub.json");
    defer allocator.free(pub_path);
    const pub_json = try std.fmt.allocPrint(allocator, "{{\"algorithm\":\"ed25519\",\"publicKeyHex\":\"{s}\"}}\n", .{key.public_hex});
    defer allocator.free(pub_json);
    try common.writeFileWithParents(pub_path, pub_json);
    return .{ .manifest = manifest, .file_count = std.mem.count(u8, manifest, "\"path\":\"") };
}

pub fn verifyAcceptanceManifest(allocator: std.mem.Allocator, artifact_dir: []const u8) !bool {
    const manifest_path = try common.pathJoin(allocator, artifact_dir, "acceptance-manifest.json");
    defer allocator.free(manifest_path);
    const sig_path = try common.pathJoin(allocator, artifact_dir, "acceptance-manifest.sig");
    defer allocator.free(sig_path);
    const pub_path = try common.pathJoin(allocator, artifact_dir, "acceptance-manifest.pub.json");
    defer allocator.free(pub_path);
    const manifest = try std.fs.cwd().readFileAlloc(allocator, manifest_path, 16 * 1024 * 1024);
    defer allocator.free(manifest);
    const sig_hex_raw = try std.fs.cwd().readFileAlloc(allocator, sig_path, 4096);
    defer allocator.free(sig_hex_raw);
    const pub_json = try std.fs.cwd().readFileAlloc(allocator, pub_path, 4096);
    defer allocator.free(pub_json);
    const pub_hex = common.extractJsonField(pub_json, "publicKeyHex") orelse return error.MissingPublicKey;
    var pub_bytes: [32]u8 = undefined;
    try common.hexToBytes(pub_hex, &pub_bytes);
    var sig_bytes: [64]u8 = undefined;
    try common.hexToBytes(std.mem.trim(u8, sig_hex_raw, " \t\r\n"), &sig_bytes);
    const public_key = try std.crypto.sign.Ed25519.PublicKey.fromBytes(pub_bytes);
    const sig = std.crypto.sign.Ed25519.Signature.fromBytes(sig_bytes);
    try sig.verify(manifest, public_key);
    return try verifyManifestFiles(allocator, artifact_dir, manifest);
}

fn buildManifest(allocator: std.mem.Allocator, artifact_dir: []const u8, public_key_id: []const u8) ![]u8 {
    const names = try acceptanceFileNames(allocator, artifact_dir);
    defer freeNames(allocator, names);
    var manifest: std.ArrayList(u8) = .empty;
    defer manifest.deinit(allocator);
    var writer = manifest.writer(allocator);
    try writer.print("{{\"schemaVersion\":\"kelp.pi.acceptance-manifest.v1\",\"publicKeyId\":\"{s}\",\"files\":[", .{public_key_id});
    for (names, 0..) |name, index| {
        if (index != 0) try writer.writeAll(",");
        const path = try common.pathJoin(allocator, artifact_dir, name);
        defer allocator.free(path);
        const hash = try common.fileHashHex(allocator, path);
        defer allocator.free(hash);
        const stat = try std.fs.cwd().statFile(path);
        try writer.print("{{\"path\":\"", .{});
        try common.writeJsonEscaped(&writer, name);
        try writer.print("\",\"size\":{},\"sha256\":\"{s}\"}}", .{ stat.size, hash });
    }
    try writer.writeAll("]}\n");
    return manifest.toOwnedSlice(allocator);
}

fn acceptanceFileNames(allocator: std.mem.Allocator, artifact_dir: []const u8) ![][]u8 {
    var names: std.ArrayList([]u8) = .empty;
    errdefer {
        for (names.items) |name| allocator.free(name);
        names.deinit(allocator);
    }
    var dir = try std.fs.cwd().openDir(artifact_dir, .{ .iterate = true });
    defer dir.close();
    var iterator = dir.iterate();
    while (try iterator.next()) |entry| {
        if (entry.kind != .file) continue;
        if (std.mem.startsWith(u8, entry.name, "acceptance-manifest.")) continue;
        if (std.mem.eql(u8, entry.name, "acceptance-manifest.json")) continue;
        try names.append(allocator, try allocator.dupe(u8, entry.name));
    }
    std.mem.sort([]u8, names.items, {}, stringLessThan);
    return names.toOwnedSlice(allocator);
}

fn verifyManifestFiles(allocator: std.mem.Allocator, artifact_dir: []const u8, manifest: []const u8) !bool {
    var rest = manifest;
    while (std.mem.indexOf(u8, rest, "\"path\":\"")) |start| {
        const after = rest[start + 8 ..];
        const end = std.mem.indexOfScalar(u8, after, '"') orelse return error.BadManifest;
        const rel = after[0..end];
        const sha_marker = "\"sha256\":\"";
        const sha_start_rel = std.mem.indexOf(u8, after[end..], sha_marker) orelse return error.BadManifest;
        const sha_after = after[end + sha_start_rel + sha_marker.len ..];
        const sha_end = std.mem.indexOfScalar(u8, sha_after, '"') orelse return error.BadManifest;
        const expected = sha_after[0..sha_end];
        const full_path = try common.pathJoin(allocator, artifact_dir, rel);
        defer allocator.free(full_path);
        const actual = try common.fileHashHex(allocator, full_path);
        defer allocator.free(actual);
        if (!std.ascii.eqlIgnoreCase(actual, expected)) return false;
        rest = sha_after[sha_end..];
    }
    return true;
}

fn stringLessThan(_: void, left: []u8, right: []u8) bool {
    return std.mem.lessThan(u8, left, right);
}

fn freeNames(allocator: std.mem.Allocator, names: [][]u8) void {
    for (names) |name| allocator.free(name);
    allocator.free(names);
}

test "acceptance manifest is signed and tamper evident" {
    const root = try common.testTempPath(std.testing.allocator, "acceptance-manifest");
    defer std.testing.allocator.free(root);
    defer std.fs.cwd().deleteTree(root) catch {};
    const data_dir = try common.pathJoin(std.testing.allocator, root, "data");
    defer std.testing.allocator.free(data_dir);
    const artifact_dir = try common.pathJoin(std.testing.allocator, root, "field-acceptance");
    defer std.testing.allocator.free(artifact_dir);
    try std.fs.cwd().makePath(artifact_dir);
    const summary = try common.pathJoin(std.testing.allocator, artifact_dir, "summary.txt");
    defer std.testing.allocator.free(summary);
    const log = try common.pathJoin(std.testing.allocator, artifact_dir, "node.log");
    defer std.testing.allocator.free(log);
    try common.writeFileWithParents(summary, "OK node node.log\n");
    try common.writeFileWithParents(log, "agent version: kelp-pi 0.2.0-zig\n");
    const result = try signAcceptanceManifest(std.testing.allocator, data_dir, artifact_dir);
    defer std.testing.allocator.free(result.manifest);
    try std.testing.expectEqual(@as(usize, 2), result.file_count);
    try std.testing.expect(try verifyAcceptanceManifest(std.testing.allocator, artifact_dir));
    try common.writeFileWithParents(summary, "OK node node.log\nFAIL tamper\n");
    try std.testing.expect(!try verifyAcceptanceManifest(std.testing.allocator, artifact_dir));
}
