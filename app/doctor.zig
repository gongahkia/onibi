const std = @import("std");
const build_options = @import("build_options");
const common = @import("common.zig");
const sqlite = @cImport({
    @cInclude("sqlite3.h");
});

pub fn doctor(args: []const []const u8) !void {
    const data_dir = common.option(args, "--data-dir") orelse common.default_data_dir;
    const policy_path = common.option(args, "--policy") orelse common.default_policy_path;
    const model_manifest = common.option(args, "--models") orelse common.default_model_manifest;
    const policy_ok = common.fileExists(policy_path);
    const model_ok = common.fileExists(model_manifest);
    const sqlite_ok = sqlite.sqlite3_libversion_number() > 0;
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print(
        "{{\"ok\":{},\"dataDir\":\"{s}\",\"checks\":[{{\"id\":\"policy-pack\",\"status\":\"{s}\"}},{{\"id\":\"model-manifest\",\"status\":\"{s}\"}},{{\"id\":\"sqlite3\",\"status\":\"{s}\"}},{{\"id\":\"llama-linked\",\"status\":\"{s}\"}}]}}\n",
        .{ policy_ok and model_ok and sqlite_ok, data_dir, if (policy_ok) "pass" else "fail", if (model_ok) "pass" else "fail", if (sqlite_ok) "pass" else "fail", if (build_options.have_llama) "pass" else "warn" },
    );
}
