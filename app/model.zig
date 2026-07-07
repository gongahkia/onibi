const std = @import("std");
const build_options = @import("build_options");
const common = @import("common.zig");

const LlamaResult = extern struct {
    ok: c_int,
    loaded: c_int,
    decoded_tokens: c_int,
    elapsed_seconds: f64,
    peak_rss_bytes: i64,
    text: [2048]u8,
    @"error": [256]u8,
};

extern fn kelp_llama_generate(model_path: [*:0]const u8, prompt: [*:0]const u8, n_predict: c_int, n_threads: c_int, result: *LlamaResult) c_int;

const default_model_id = "qwen3-0.6b-q4_k_m";
const default_model_smoke_prompt =
    \\<|im_start|>user
    \\/no_think Reply with exactly: ready<|im_end|>
    \\<|im_start|>assistant
;

const ModelEntry = struct {
    id: []const u8 = "",
    url: []const u8 = "",
    sha256: []const u8 = "",
    ram_floor_mb: u64 = 0,
    primary: bool = false,
};

const ModelManifest = struct {
    models: [16]ModelEntry = undefined,
    len: usize = 0,

    fn push(self: *ModelManifest, model: ModelEntry) void {
        if (model.id.len == 0 or self.len >= self.models.len) return;
        self.models[self.len] = model;
        self.len += 1;
    }

    fn find(self: ModelManifest, id: []const u8) ?ModelEntry {
        var index: usize = 0;
        while (index < self.len) : (index += 1) {
            if (std.mem.eql(u8, self.models[index].id, id)) return self.models[index];
        }
        return null;
    }
};

pub fn modelCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0) return common.fail("usage: kelp-pi model <fetch|warm|verify|prompt> --id MODEL_ID", 64);
    const sub = args[0];
    const id = common.option(args[1..], "--id") orelse default_model_id;
    const manifest_path = common.option(args[1..], "--manifest") orelse common.default_model_manifest;
    const manifest_text = try std.fs.cwd().readFileAlloc(allocator, manifest_path, 1024 * 1024);
    defer allocator.free(manifest_text);
    const manifest = parseModelManifest(manifest_text);
    const model = manifest.find(id) orelse return common.printJsonStatus(false, "missing-model", "model id not in manifest");
    if (model.sha256.len == 0) return common.printJsonStatus(false, "missing-sha256", "model manifest sha256 is blank");
    const data_dir = common.option(args[1..], "--data-dir") orelse ".kelp-pi";
    const default_path = try modelCachePath(allocator, data_dir, model.url);
    defer allocator.free(default_path);
    const model_path = common.option(args[1..], "--model-path") orelse default_path;
    if (std.mem.eql(u8, sub, "fetch")) return modelFetch(allocator, model, model_path);
    if (std.mem.eql(u8, sub, "verify")) return modelVerify(allocator, model, model_path, false, null, 0, 0);
    if (std.mem.eql(u8, sub, "warm")) return modelVerify(allocator, model, model_path, true, common.option(args[1..], "--prompt") orelse default_model_smoke_prompt, common.parseUsize(common.option(args[1..], "--n-predict") orelse "1", 1), common.parseUsize(common.option(args[1..], "--threads") orelse "2", 2));
    if (std.mem.eql(u8, sub, "prompt")) return modelVerify(allocator, model, model_path, true, common.option(args[1..], "--prompt") orelse default_model_smoke_prompt, common.parseUsize(common.option(args[1..], "--n-predict") orelse "32", 32), common.parseUsize(common.option(args[1..], "--threads") orelse "2", 2));
    return common.fail("usage: kelp-pi model <fetch|warm|verify|prompt> --id MODEL_ID", 64);
}

fn modelFetch(allocator: std.mem.Allocator, model: ModelEntry, model_path: []const u8) !void {
    if (common.fileExists(model_path)) {
        return modelVerify(allocator, model, model_path, false, null, 0, 0);
    }
    if (std.fs.path.dirname(model_path)) |parent| try std.fs.cwd().makePath(parent);
    const result = try std.process.Child.run(.{ .allocator = allocator, .argv = &.{ "curl", "-fL", "--retry", "3", "-o", model_path, model.url }, .max_output_bytes = 1024 * 1024 });
    defer allocator.free(result.stdout);
    defer allocator.free(result.stderr);
    const ok = switch (result.term) {
        .Exited => |code| code == 0,
        else => false,
    };
    if (!ok) return common.fail("model download failed", 77);
    return modelVerify(allocator, model, model_path, false, null, 0, 0);
}

fn modelVerify(allocator: std.mem.Allocator, model: ModelEntry, model_path: []const u8, warm: bool, prompt: ?[]const u8, n_predict: usize, n_threads: usize) !void {
    if (!common.fileExists(model_path)) return common.printJsonStatus(false, "missing-model-file", "model path does not exist");
    const actual = try common.fileHashHex(allocator, model_path);
    defer allocator.free(actual);
    if (!std.ascii.eqlIgnoreCase(actual, model.sha256)) return common.printJsonStatus(false, "sha256-mismatch", "model file hash does not match manifest");
    if (!try common.fileHasMagic(model_path, "GGUF")) return common.printJsonStatus(false, "invalid-gguf", "model file missing GGUF magic");
    if (!ramGateAllows(model.ram_floor_mb)) return common.printJsonStatus(false, "ram-gate", "detected RAM below model floor");
    var out = std.fs.File.stdout().deprecatedWriter();
    if (warm and build_options.have_llama) {
        return modelGenerate(allocator, model, model_path, actual, prompt orelse "ready", n_predict, n_threads);
    } else {
        try out.print("{{\"ok\":true,\"id\":\"{s}\",\"path\":\"{s}\",\"sha256\":\"{s}\",\"runtime\":\"llama.cpp\",\"loaded\":false,\"reason\":\"libllama not linked in this build\"}}\n", .{ model.id, model_path, actual });
    }
}

fn modelGenerate(allocator: std.mem.Allocator, model: ModelEntry, model_path: []const u8, actual_hash: []const u8, prompt: []const u8, n_predict_raw: usize, n_threads_raw: usize) !void {
    const model_path_z = try allocator.dupeZ(u8, model_path);
    defer allocator.free(model_path_z);
    const prompt_z = try allocator.dupeZ(u8, prompt);
    defer allocator.free(prompt_z);
    var result: LlamaResult = undefined;
    const n_predict = @min(n_predict_raw, 512);
    const n_threads = @max(n_threads_raw, 1);
    const rc = kelp_llama_generate(model_path_z.ptr, prompt_z.ptr, @intCast(n_predict), @intCast(n_threads), &result);
    var out = std.fs.File.stdout().deprecatedWriter();
    if (rc != 0 or result.ok == 0) {
        try out.print("{{\"ok\":false,\"id\":\"{s}\",\"path\":\"{s}\",\"runtime\":\"llama.cpp\",\"loaded\":{},\"peakRssBytes\":{},\"reason\":\"", .{ model.id, model_path, result.loaded != 0, result.peak_rss_bytes });
        try common.writeJsonEscaped(&out, std.mem.sliceTo(result.@"error"[0..], 0));
        try out.print("\"}}\n", .{});
        return;
    }
    try out.print("{{\"ok\":true,\"id\":\"{s}\",\"path\":\"{s}\",\"sha256\":\"{s}\",\"runtime\":\"llama.cpp\",\"loaded\":true,\"decodedTokens\":{},\"elapsedSeconds\":{d:.3},\"peakRssBytes\":{},\"text\":\"", .{ model.id, model_path, actual_hash, result.decoded_tokens, result.elapsed_seconds, result.peak_rss_bytes });
    try common.writeJsonEscaped(&out, std.mem.sliceTo(result.text[0..], 0));
    try out.print("\"}}\n", .{});
}

fn parseModelManifest(text: []const u8) ModelManifest {
    var manifest = ModelManifest{};
    var current = ModelEntry{};
    var in_model = false;
    var lines = std.mem.splitScalar(u8, text, '\n');
    while (lines.next()) |raw| {
        const line = std.mem.trim(u8, raw, " \t\r");
        if (line.len == 0 or line[0] == '#') continue;
        if (std.mem.eql(u8, line, "[[model]]")) {
            if (in_model) manifest.push(current);
            current = ModelEntry{};
            in_model = true;
            continue;
        }
        if (common.startsWith(line, "id =")) current.id = common.quotedValue(line) orelse "";
        if (common.startsWith(line, "url =")) current.url = common.quotedValue(line) orelse "";
        if (common.startsWith(line, "sha256 =")) current.sha256 = common.quotedValue(line) orelse "";
        if (common.startsWith(line, "primary =")) current.primary = std.mem.indexOf(u8, line, "true") != null;
        if (common.startsWith(line, "ram_floor_mb =")) current.ram_floor_mb = common.parseU64AfterEquals(line, 0);
    }
    if (in_model) manifest.push(current);
    return manifest;
}

fn modelCachePath(allocator: std.mem.Allocator, data_dir: []const u8, url: []const u8) ![]u8 {
    const slash = std.mem.lastIndexOfScalar(u8, url, '/') orelse 0;
    return std.fmt.allocPrint(allocator, "{s}/models/{s}", .{ data_dir, url[slash + 1 ..] });
}

fn ramGateAllows(floor_mb: u64) bool {
    if (floor_mb == 0) return true;
    const meminfo_path = std.process.getEnvVarOwned(std.heap.page_allocator, "KELP_PI_MEMINFO_PATH") catch "/proc/meminfo";
    defer if (!std.mem.eql(u8, meminfo_path, "/proc/meminfo")) std.heap.page_allocator.free(meminfo_path);
    const content = std.fs.cwd().readFileAlloc(std.heap.page_allocator, meminfo_path, 1024 * 64) catch return true;
    defer std.heap.page_allocator.free(content);
    const marker = "MemTotal:";
    const start = std.mem.indexOf(u8, content, marker) orelse return true;
    const after = std.mem.trimLeft(u8, content[start + marker.len ..], " \t");
    var end: usize = 0;
    while (end < after.len and std.ascii.isDigit(after[end])) : (end += 1) {}
    const kb = std.fmt.parseInt(u64, after[0..end], 10) catch return true;
    return kb / 1024 >= floor_mb;
}

test "model manifest rejects blank sha by parsing concrete sha" {
    const manifest = parseModelManifest(
        \\[[model]]
        \\id = "m"
        \\url = "https://example.test/m.gguf"
        \\sha256 = "abc"
        \\primary = true
        \\ram_floor_mb = 1024
    );
    const model = manifest.find("m").?;
    try std.testing.expect(std.mem.eql(u8, model.sha256, "abc"));
    try std.testing.expectEqual(@as(u64, 1024), model.ram_floor_mb);
}
