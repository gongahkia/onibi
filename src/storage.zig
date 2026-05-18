const std = @import("std");
const domain = @import("domain.zig");
const json = @import("json_output.zig");

const BACKUP_RETENTION: usize = 10;
const LOCK_ATTEMPTS: usize = 5;
const LOCK_RETRY_DELAY_NS: u64 = 20 * std.time.ns_per_ms;
const STALE_LOCK_AFTER_SECS: i64 = 30;

pub const JsonFileStorage = struct {
    allocator: std.mem.Allocator,
    root: []const u8,

    pub fn fromEnv(allocator: std.mem.Allocator) !JsonFileStorage {
        return .{ .allocator = allocator, .root = try resolveDataRoot(allocator) };
    }

    pub fn at(allocator: std.mem.Allocator, root: []const u8) !JsonFileStorage {
        return .{ .allocator = allocator, .root = try allocator.dupe(u8, root) };
    }

    pub fn init(self: JsonFileStorage) ![]const u8 {
        return self.ensureDefaultDataFile();
    }

    pub fn load(self: JsonFileStorage) !domain.AppState {
        const data_file = try self.ensureDefaultDataFile();
        const contents = try std.fs.cwd().readFileAlloc(self.allocator, data_file, 64 * 1024 * 1024);
        if (std.mem.trim(u8, contents, " \t\r\n").len == 0) return domain.AppState.init(self.allocator);
        return parseState(self.allocator, contents) catch |err| {
            return self.recoverFromBackup(data_file, err);
        };
    }

    pub fn save(self: JsonFileStorage, state: *const domain.AppState) !void {
        _ = try self.ensureDefaultDataFile();
        const lock_path = try self.acquireWriteLock();
        defer std.fs.cwd().deleteFile(lock_path) catch {};
        try self.writeStateFile(state);
        _ = try self.snapshotCurrentData();
    }

    pub fn dataFile(self: JsonFileStorage) ![]const u8 {
        return join(self.allocator, self.root, "data.json");
    }

    pub fn rootDir(self: JsonFileStorage) []const u8 {
        return self.root;
    }

    pub fn backupDir(self: JsonFileStorage) ![]const u8 {
        return join(self.allocator, self.root, "backups");
    }

    pub fn lockFile(self: JsonFileStorage) ![]const u8 {
        return join(self.allocator, self.root, "data.lock");
    }

    pub fn exportTo(self: JsonFileStorage, output: []const u8) ![]const u8 {
        const data_file = try self.ensureDefaultDataFile();
        try ensureParent(output);
        _ = try std.fs.cwd().copyFile(data_file, std.fs.cwd(), output, .{});
        return self.allocator.dupe(u8, output);
    }

    pub fn createBackupSnapshot(self: JsonFileStorage) ![]const u8 {
        _ = try self.ensureDefaultDataFile();
        const lock_path = try self.acquireWriteLock();
        defer std.fs.cwd().deleteFile(lock_path) catch {};
        return (try self.snapshotCurrentData()) orelse error.BackupFailed;
    }

    fn ensureDefaultDataFile(self: JsonFileStorage) ![]const u8 {
        const data_file = try self.dataFile();
        try ensureParent(data_file);
        try std.fs.cwd().makePath(try self.backupDir());
        try std.fs.cwd().makePath(try join(self.allocator, self.root, "corrupt"));
        if (!exists(data_file)) {
            var state = domain.AppState.init(self.allocator);
            try self.writeStateFile(&state);
        }
        return data_file;
    }

    fn writeStateFile(self: JsonFileStorage, state: *const domain.AppState) !void {
        const data_file = try self.dataFile();
        const temp = try std.fmt.allocPrint(self.allocator, "{s}.tmp", .{data_file});
        const contents = try renderStateJson(self.allocator, state);
        try std.fs.cwd().writeFile(.{ .sub_path = temp, .data = contents });
        if (exists(data_file)) std.fs.cwd().deleteFile(data_file) catch {};
        try std.fs.cwd().rename(temp, data_file);
    }

    fn acquireWriteLock(self: JsonFileStorage) ![]const u8 {
        const lock_file = try self.lockFile();
        try ensureParent(lock_file);
        var attempt: usize = 0;
        while (attempt < LOCK_ATTEMPTS) : (attempt += 1) {
            const file = std.fs.cwd().createFile(lock_file, .{ .exclusive = true }) catch |err| switch (err) {
                error.PathAlreadyExists => {
                    if (try self.lockIsStale(lock_file)) {
                        std.fs.cwd().deleteFile(lock_file) catch {};
                        continue;
                    }
                    if (attempt + 1 == LOCK_ATTEMPTS) return error.StorageLocked;
                    std.Thread.sleep(LOCK_RETRY_DELAY_NS);
                    continue;
                },
                else => return err,
            };
            defer file.close();
            var buf: [128]u8 = undefined;
            const line = try std.fmt.bufPrint(&buf, "pid={d}\ncreated_at={d}\n", .{ 0, std.time.timestamp() });
            try file.writeAll(line);
            return lock_file;
        }
        return error.StorageLocked;
    }

    fn lockIsStale(self: JsonFileStorage, path: []const u8) !bool {
        _ = self;
        const stat = try std.fs.cwd().statFile(path);
        return std.time.timestamp() - stat.mtime >= STALE_LOCK_AFTER_SECS;
    }

    fn snapshotCurrentData(self: JsonFileStorage) !?[]const u8 {
        const data_file = try self.dataFile();
        if (!exists(data_file)) return null;
        const backup = try std.fmt.allocPrint(self.allocator, "{s}/data-{d}.json", .{ try self.backupDir(), std.time.nanoTimestamp() });
        try ensureParent(backup);
        _ = try std.fs.cwd().copyFile(data_file, std.fs.cwd(), backup, .{});
        try self.pruneOldBackups();
        return backup;
    }

    fn pruneOldBackups(self: JsonFileStorage) !void {
        var backups = try self.listBackups();
        defer backups.deinit(self.allocator);
        std.mem.sort([]const u8, backups.items, {}, lessString);
        if (backups.items.len <= BACKUP_RETENTION) return;
        const obsolete = backups.items.len - BACKUP_RETENTION;
        for (backups.items[0..obsolete]) |path| std.fs.cwd().deleteFile(path) catch {};
    }

    fn listBackups(self: JsonFileStorage) !std.ArrayList([]const u8) {
        var out: std.ArrayList([]const u8) = .empty;
        const dir_path = try self.backupDir();
        var dir = std.fs.cwd().openDir(dir_path, .{ .iterate = true }) catch return out;
        defer dir.close();
        var it = dir.iterate();
        while (try it.next()) |entry| {
            if (entry.kind != .file or !std.mem.endsWith(u8, entry.name, ".json")) continue;
            try out.append(self.allocator, try join(self.allocator, dir_path, entry.name));
        }
        return out;
    }

    fn recoverFromBackup(self: JsonFileStorage, data_file: []const u8, parse_error: anyerror) !domain.AppState {
        const corrupt = try std.fmt.allocPrint(self.allocator, "{s}/corrupt/data-corrupt-{d}.json", .{ self.root, std.time.timestamp() });
        try ensureParent(corrupt);
        if (exists(data_file)) std.fs.cwd().rename(data_file, corrupt) catch {};
        var backups = try self.listBackups();
        defer backups.deinit(self.allocator);
        std.mem.sort([]const u8, backups.items, {}, greaterString);
        for (backups.items) |backup| {
            const contents = std.fs.cwd().readFileAlloc(self.allocator, backup, 64 * 1024 * 1024) catch continue;
            if (parseState(self.allocator, contents)) |state| {
                try self.writeStateFile(&state);
                return state;
            } else |_| {}
        }
        return parse_error;
    }
};

const RawAppState = struct {
    schema_version: u32 = 1,
    next_task_id: u64 = 1,
    next_project_id: u64 = 1,
    tasks: []domain.Task = &.{},
    projects: []domain.Project = &.{},
};

fn parseState(allocator: std.mem.Allocator, contents: []const u8) !domain.AppState {
    const raw = try std.json.parseFromSliceLeaky(RawAppState, allocator, contents, .{
        .ignore_unknown_fields = true,
        .allocate = .alloc_always,
    });
    if (raw.schema_version > domain.CURRENT_APP_SCHEMA_VERSION) return error.FutureAppSchema;
    var state = domain.AppState.init(allocator);
    state.schema_version = domain.CURRENT_APP_SCHEMA_VERSION;
    state.next_task_id = raw.next_task_id;
    state.next_project_id = raw.next_project_id;
    for (raw.tasks) |task| try state.tasks.append(allocator, task);
    for (raw.projects) |project| try state.projects.append(allocator, project);
    return state;
}

pub fn renderStateJson(allocator: std.mem.Allocator, state: *const domain.AppState) ![]const u8 {
    var out: std.ArrayList(u8) = .empty;
    try out.print(allocator,
        \\{{
        \\  "schema_version": {d},
        \\  "next_task_id": {d},
        \\  "next_project_id": {d},
        \\  "tasks": [
    , .{ domain.CURRENT_APP_SCHEMA_VERSION, state.next_task_id, state.next_project_id });
    for (state.tasks.items, 0..) |task, index| {
        if (index > 0) try out.appendSlice(allocator, ",");
        try out.appendSlice(allocator, "\n    ");
        try appendTaskJson(&out, allocator, task);
    }
    try out.appendSlice(allocator, "\n  ],\n  \"projects\": [");
    for (state.projects.items, 0..) |project, index| {
        if (index > 0) try out.appendSlice(allocator, ",");
        try out.appendSlice(allocator, "\n    ");
        try appendProjectJson(&out, allocator, project);
    }
    try out.appendSlice(allocator, "\n  ]\n}\n");
    return out.toOwnedSlice(allocator);
}

fn appendTaskJson(out: *std.ArrayList(u8), allocator: std.mem.Allocator, task: domain.Task) !void {
    try out.print(allocator, "{{\"id\": {d}, \"title\": ", .{task.id});
    try json.appendEscaped(out, allocator, task.title);
    try out.appendSlice(allocator, ", \"notes\": ");
    try json.appendOptionalString(out, allocator, task.notes);
    try out.appendSlice(allocator, ", \"project_id\": ");
    if (task.project_id) |id| try out.print(allocator, "{d}", .{id}) else try out.appendSlice(allocator, "null");
    try out.print(allocator, ", \"status\": \"{s}\", \"priority\": \"{s}\", \"tags\": [", .{ task.status.label(), task.priority.label() });
    for (task.tags, 0..) |tag, index| {
        if (index > 0) try out.appendSlice(allocator, ", ");
        try json.appendEscaped(out, allocator, tag);
    }
    try out.appendSlice(allocator, "], \"due_date\": ");
    try json.appendOptionalString(out, allocator, task.due_date);
    try out.appendSlice(allocator, ", \"recurrence\": ");
    if (task.recurrence) |rule| try json.appendEscaped(out, allocator, rule.label()) else try out.appendSlice(allocator, "null");
    try out.appendSlice(allocator, ", \"created_on\": ");
    try json.appendEscaped(out, allocator, task.created_on);
    try out.appendSlice(allocator, ", \"updated_on\": ");
    try json.appendEscaped(out, allocator, task.updated_on);
    try out.appendSlice(allocator, ", \"completed_on\": ");
    try json.appendOptionalString(out, allocator, task.completed_on);
    try out.appendSlice(allocator, ", \"archived_on\": ");
    try json.appendOptionalString(out, allocator, task.archived_on);
    try out.appendSlice(allocator, ", \"waiting_until\": ");
    try json.appendOptionalString(out, allocator, task.waiting_until);
    try out.appendSlice(allocator, ", \"blocked_reason\": ");
    try json.appendOptionalString(out, allocator, task.blocked_reason);
    try out.appendSlice(allocator, ", \"depends_on\": [");
    for (task.depends_on, 0..) |dep, index| {
        if (index > 0) try out.appendSlice(allocator, ", ");
        try out.print(allocator, "{d}", .{dep});
    }
    try out.appendSlice(allocator, "]}");
}

fn appendProjectJson(out: *std.ArrayList(u8), allocator: std.mem.Allocator, project: domain.Project) !void {
    try out.print(allocator, "{{\"id\": {d}, \"name\": ", .{project.id});
    try json.appendEscaped(out, allocator, project.name);
    try out.appendSlice(allocator, ", \"description\": ");
    try json.appendOptionalString(out, allocator, project.description);
    try out.print(allocator, ", \"status\": \"{s}\", \"created_on\": ", .{project.status.label()});
    try json.appendEscaped(out, allocator, project.created_on);
    try out.appendSlice(allocator, ", \"updated_on\": ");
    try json.appendEscaped(out, allocator, project.updated_on);
    try out.appendSlice(allocator, ", \"archived_on\": ");
    try json.appendOptionalString(out, allocator, project.archived_on);
    try out.appendSlice(allocator, ", \"deadline\": ");
    try json.appendOptionalString(out, allocator, project.deadline);
    try out.appendSlice(allocator, "}");
}

fn resolveDataRoot(allocator: std.mem.Allocator) ![]const u8 {
    if (std.process.getEnvVarOwned(allocator, "KELP_DATA_DIR")) |value| return value else |_| {}
    if (std.process.getEnvVarOwned(allocator, "XDG_DATA_HOME")) |value| return join(allocator, value, "kelp") else |_| {}
    if (std.process.getEnvVarOwned(allocator, "HOME")) |home| return std.fs.path.join(allocator, &.{ home, ".local", "share", "kelp" }) else |_| {}
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

fn lessString(_: void, a: []const u8, b: []const u8) bool {
    return std.mem.lessThan(u8, a, b);
}

fn greaterString(_: void, a: []const u8, b: []const u8) bool {
    return std.mem.lessThan(u8, b, a);
}
