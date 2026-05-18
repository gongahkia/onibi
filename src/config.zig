const std = @import("std");

pub const CURRENT_CONFIG_SCHEMA_VERSION: u32 = 1;

pub const TaskSortKey = enum {
    due,
    priority,
    updated,
    title,

    pub fn label(self: TaskSortKey) []const u8 {
        return @tagName(self);
    }
};

pub const AppConfig = struct {
    schema_version: u32 = CURRENT_CONFIG_SCHEMA_VERSION,
    default_upcoming_days: i64 = 7,
    default_task_sort: TaskSortKey = .due,
    default_json_output: bool = false,
};

pub const Store = struct {
    allocator: std.mem.Allocator,
    root: []const u8,
    legacy_data_root: ?[]const u8 = null,

    pub fn at(allocator: std.mem.Allocator, root: []const u8) !Store {
        return .{ .allocator = allocator, .root = try allocator.dupe(u8, root) };
    }

    pub fn fromEnvWithDataRoot(allocator: std.mem.Allocator, data_root: []const u8, colocate: bool) !Store {
        const root = try resolveConfigRoot(allocator, if (colocate) data_root else null);
        const legacy = if (!colocate and !std.mem.eql(u8, root, data_root)) try allocator.dupe(u8, data_root) else null;
        return .{ .allocator = allocator, .root = root, .legacy_data_root = legacy };
    }

    pub fn configFile(self: Store) ![]const u8 {
        return join(self.allocator, self.root, "config.json");
    }

    pub fn init(self: Store) ![]const u8 {
        try self.migrateLegacyConfigIfNeeded();
        const file = try self.configFile();
        try ensureParent(file);
        if (!exists(file)) try self.save(.{});
        return file;
    }

    pub fn load(self: Store) !AppConfig {
        const file = try self.init();
        const contents = std.fs.cwd().readFileAlloc(self.allocator, file, 1024 * 1024) catch |err| switch (err) {
            error.FileNotFound => return .{},
            else => return err,
        };
        if (std.mem.trim(u8, contents, " \t\r\n").len == 0) return .{};
        const parsed = std.json.parseFromSliceLeaky(AppConfig, self.allocator, contents, .{
            .ignore_unknown_fields = true,
            .allocate = .alloc_always,
        }) catch |err| {
            try self.recoverCorruptConfig(file, err);
            return err;
        };
        if (parsed.schema_version > CURRENT_CONFIG_SCHEMA_VERSION) return error.FutureConfigSchema;
        return .{
            .schema_version = CURRENT_CONFIG_SCHEMA_VERSION,
            .default_upcoming_days = if (parsed.default_upcoming_days == 0) 7 else parsed.default_upcoming_days,
            .default_task_sort = parsed.default_task_sort,
            .default_json_output = parsed.default_json_output,
        };
    }

    pub fn save(self: Store, config: AppConfig) !void {
        const file = try self.configFile();
        try ensureParent(file);
        const temp = try std.fmt.allocPrint(self.allocator, "{s}.tmp", .{file});
        var out: std.ArrayList(u8) = .empty;
        defer out.deinit(self.allocator);
        try out.print(self.allocator,
            \\{{
            \\  "schema_version": {d},
            \\  "default_upcoming_days": {d},
            \\  "default_task_sort": "{s}",
            \\  "default_json_output": {}
            \\}}
            \\
        , .{ CURRENT_CONFIG_SCHEMA_VERSION, config.default_upcoming_days, config.default_task_sort.label(), config.default_json_output });
        try std.fs.cwd().writeFile(.{ .sub_path = temp, .data = out.items });
        if (exists(file)) std.fs.cwd().deleteFile(file) catch {};
        try std.fs.cwd().rename(temp, file);
    }

    fn migrateLegacyConfigIfNeeded(self: Store) !void {
        const legacy_root = self.legacy_data_root orelse return;
        const target = try self.configFile();
        const legacy = try join(self.allocator, legacy_root, "config.json");
        if (std.mem.eql(u8, target, legacy) or exists(target) or !exists(legacy)) return;
        try ensureParent(target);
        try std.fs.cwd().rename(legacy, target);
    }

    fn recoverCorruptConfig(self: Store, file: []const u8, parse_error: anytype) !void {
        _ = @errorName(parse_error);
        const corrupt_dir = try join(self.allocator, self.root, "corrupt");
        try std.fs.cwd().makePath(corrupt_dir);
        const corrupt = try std.fmt.allocPrint(self.allocator, "{s}/config-corrupt-{d}.json", .{ corrupt_dir, std.time.timestamp() });
        if (exists(file)) try std.fs.cwd().rename(file, corrupt);
        try self.save(.{});
    }
};

pub fn parseSortKey(value: []const u8) ?TaskSortKey {
    inline for (@typeInfo(TaskSortKey).@"enum".fields) |field| {
        if (std.ascii.eqlIgnoreCase(value, field.name)) return @enumFromInt(field.value);
    }
    return null;
}

fn resolveConfigRoot(allocator: std.mem.Allocator, data_root_hint: ?[]const u8) ![]const u8 {
    if (std.process.getEnvVarOwned(allocator, "KELP_CONFIG_DIR")) |value| return value else |_| {}
    if (data_root_hint) |value| return allocator.dupe(u8, value);
    if (std.process.getEnvVarOwned(allocator, "KELP_DATA_DIR")) |value| return value else |_| {}
    if (std.process.getEnvVarOwned(allocator, "XDG_CONFIG_HOME")) |value| return join(allocator, value, "kelp") else |_| {}
    if (std.process.getEnvVarOwned(allocator, "HOME")) |home| return join3(allocator, home, ".config", "kelp") else |_| {}
    const cwd = try std.fs.cwd().realpathAlloc(allocator, ".");
    return join(allocator, cwd, ".kelp");
}

fn ensureParent(path: []const u8) !void {
    if (std.fs.path.dirname(path)) |parent| try std.fs.cwd().makePath(parent);
}

fn exists(path: []const u8) bool {
    std.fs.cwd().access(path, .{}) catch return false;
    return true;
}

fn join(allocator: std.mem.Allocator, a: []const u8, b: []const u8) ![]const u8 {
    return std.fs.path.join(allocator, &.{ a, b });
}

fn join3(allocator: std.mem.Allocator, a: []const u8, b: []const u8, c: []const u8) ![]const u8 {
    return std.fs.path.join(allocator, &.{ a, b, c });
}
