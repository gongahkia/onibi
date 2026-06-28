const std = @import("std");

// Zig toolchain pin for the local-only Kelp Pi runtime: 0.16.0.
// The source is kept compatible with 0.15.2 while local machines catch up.
pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});
    const have_llama = b.option(bool, "llama", "Link libllama and enable real llama.cpp model load") orelse false;

    const options = b.addOptions();
    options.addOption(bool, "have_llama", have_llama);

    const root = b.createModule(.{
        .root_source_file = b.path("app/main.zig"),
        .target = target,
        .optimize = optimize,
    });
    root.addOptions("build_options", options);
    const exe = b.addExecutable(.{
        .name = "kelp-pi",
        .root_module = root,
    });
    exe.linkLibC();
    exe.linkSystemLibrary("sqlite3");
    if (have_llama) {
        exe.addIncludePath(b.path("vendor/llama.cpp/include"));
        exe.linkSystemLibrary("llama");
    }
    b.installArtifact(exe);

    const test_root = b.createModule(.{
        .root_source_file = b.path("app/main.zig"),
        .target = target,
        .optimize = optimize,
    });
    test_root.addOptions("build_options", options);
    const tests = b.addTest(.{ .root_module = test_root });
    tests.linkLibC();
    tests.linkSystemLibrary("sqlite3");
    if (have_llama) {
        tests.addIncludePath(b.path("vendor/llama.cpp/include"));
        tests.linkSystemLibrary("llama");
    }
    const run_tests = b.addRunArtifact(tests);
    const test_step = b.step("test", "Run Zig unit tests");
    test_step.dependOn(&run_tests.step);
}
