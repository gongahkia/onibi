const std = @import("std");

// Zig toolchain pin for the local-only Kelp Pi runtime: 0.16.0.
// The source is kept compatible with 0.15.2 while local machines catch up.
pub fn build(b: *std.Build) void {
    const target = b.standardTargetOptions(.{});
    const optimize = b.standardOptimizeOption(.{});
    const have_llama = b.option(bool, "llama", "Link libllama and enable real llama.cpp model load") orelse false;
    const llama_prefix = b.option([]const u8, "llama-prefix", "Prefix containing llama.cpp include/ and lib/") orelse ".kelp-pi/llama/host";
    const system_include_dir = b.option([]const u8, "system-include-dir", "Extra directory containing target system headers") orelse "";
    const system_lib_dir = b.option([]const u8, "system-lib-dir", "Extra directory containing target system libraries") orelse "";

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
    addSystemIncludePath(exe, system_include_dir);
    addSystemLibraryPath(exe, system_lib_dir);
    exe.linkSystemLibrary("sqlite3");
    if (have_llama) addLlamaBridge(b, exe, llama_prefix);
    b.installArtifact(exe);

    const test_root = b.createModule(.{
        .root_source_file = b.path("app/main.zig"),
        .target = target,
        .optimize = optimize,
    });
    test_root.addOptions("build_options", options);
    const tests = b.addTest(.{ .root_module = test_root });
    tests.linkLibC();
    addSystemIncludePath(tests, system_include_dir);
    addSystemLibraryPath(tests, system_lib_dir);
    tests.linkSystemLibrary("sqlite3");
    if (have_llama) addLlamaBridge(b, tests, llama_prefix);
    const run_tests = b.addRunArtifact(tests);
    const test_step = b.step("test", "Run Zig unit tests");
    test_step.dependOn(&run_tests.step);
}

fn addSystemIncludePath(compile: *std.Build.Step.Compile, dir: []const u8) void {
    if (dir.len != 0) compile.addSystemIncludePath(.{ .cwd_relative = dir });
}

fn addSystemLibraryPath(compile: *std.Build.Step.Compile, dir: []const u8) void {
    if (dir.len != 0) compile.addLibraryPath(.{ .cwd_relative = dir });
}

fn addLlamaBridge(b: *std.Build, compile: *std.Build.Step.Compile, prefix: []const u8) void {
    const lib_dir = b.pathJoin(&.{ prefix, "lib" });
    const include_dir = b.pathJoin(&.{ prefix, "include" });
    compile.addIncludePath(b.path("app"));
    compile.addIncludePath(.{ .cwd_relative = include_dir });
    compile.addIncludePath(b.path("vendor/llama.cpp/include"));
    compile.addCSourceFile(.{
        .file = b.path("app/llama_bridge.c"),
        .flags = &.{ "-std=c11", "-Wall", "-Wextra" },
    });
    compile.addLibraryPath(.{ .cwd_relative = lib_dir });
    compile.addRPath(.{ .cwd_relative = lib_dir });
    compile.linkSystemLibrary("llama");
    compile.linkSystemLibrary("ggml");
    compile.linkSystemLibrary("ggml-base");
    compile.linkSystemLibrary("ggml-cpu");
    compile.linkLibCpp();
}
