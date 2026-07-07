const std = @import("std");
const common = @import("common.zig");

pub const KeyMaterial = struct {
    key_pair: std.crypto.sign.Ed25519.KeyPair,
    public_hex: []u8,
    private_hex: []u8,
};

pub fn keygen(allocator: std.mem.Allocator, args: []const []const u8) !void {
    const data_dir = common.option(args, "--data-dir") orelse common.default_data_dir;
    const label = common.option(args, "--label") orelse "kelp-pi";
    var key = try generateKeyMaterial(allocator);
    defer freeKeyMaterial(allocator, &key);
    const key_dir = try common.pathJoin(allocator, data_dir, "keys");
    defer allocator.free(key_dir);
    try std.fs.cwd().makePath(key_dir);
    const key_path = try common.pathJoin(allocator, key_dir, "pi-ed25519.key.json");
    defer allocator.free(key_path);
    const payload = try std.fmt.allocPrint(
        allocator,
        "{{\"schemaVersion\":\"kelp.pi.key.v1\",\"algorithm\":\"ed25519\",\"label\":\"{s}\",\"publicKeyHex\":\"{s}\",\"privateKeyHex\":\"{s}\"}}\n",
        .{ label, key.public_hex, key.private_hex },
    );
    defer allocator.free(payload);
    try common.writeFileWithParents(key_path, payload);
    common.chmod600(key_path);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"keyPath\":\"{s}\",\"label\":\"{s}\",\"publicKeyHex\":\"{s}\"}}\n", .{ key_path, label, key.public_hex });
}

pub fn generateKeyMaterial(allocator: std.mem.Allocator) !KeyMaterial {
    const key_pair = std.crypto.sign.Ed25519.KeyPair.generate();
    const private_bytes = key_pair.secret_key.toBytes();
    const public_bytes = key_pair.public_key.toBytes();
    return .{
        .key_pair = key_pair,
        .public_hex = try common.hexAlloc(allocator, &public_bytes),
        .private_hex = try common.hexAlloc(allocator, &private_bytes),
    };
}

pub fn loadOrCreateKey(allocator: std.mem.Allocator, key_dir: []const u8) !KeyMaterial {
    try std.fs.cwd().makePath(key_dir);
    const key_path = try common.pathJoin(allocator, key_dir, "pi-ed25519.key.json");
    defer allocator.free(key_path);
    if (!common.fileExists(key_path)) {
        const key = try generateKeyMaterial(allocator);
        const payload = try std.fmt.allocPrint(allocator, "{{\"schemaVersion\":\"kelp.pi.key.v1\",\"algorithm\":\"ed25519\",\"label\":\"kelp-pi\",\"publicKeyHex\":\"{s}\",\"privateKeyHex\":\"{s}\"}}\n", .{ key.public_hex, key.private_hex });
        defer allocator.free(payload);
        try common.writeFileWithParents(key_path, payload);
        common.chmod600(key_path);
        return key;
    }
    const content = try std.fs.cwd().readFileAlloc(allocator, key_path, 4096);
    defer allocator.free(content);
    const private_hex = common.extractJsonField(content, "privateKeyHex") orelse return error.MissingPrivateKey;
    var private_bytes: [64]u8 = undefined;
    try common.hexToBytes(private_hex, &private_bytes);
    const secret = try std.crypto.sign.Ed25519.SecretKey.fromBytes(private_bytes);
    const key_pair = try std.crypto.sign.Ed25519.KeyPair.fromSecretKey(secret);
    const public_bytes = key_pair.public_key.toBytes();
    const private_again = key_pair.secret_key.toBytes();
    return .{
        .key_pair = key_pair,
        .public_hex = try common.hexAlloc(allocator, &public_bytes),
        .private_hex = try common.hexAlloc(allocator, &private_again),
    };
}

pub fn freeKeyMaterial(allocator: std.mem.Allocator, key: *KeyMaterial) void {
    allocator.free(key.public_hex);
    allocator.free(key.private_hex);
}
