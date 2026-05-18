const std = @import("std");
const config = @import("config.zig");
const dates = @import("dates.zig");
const domain = @import("domain.zig");
const errors = @import("errors.zig");
const json = @import("json_output.zig");
const legacy = @import("legacy_import.zig");
const render = @import("plain_render.zig");
const storage_mod = @import("storage.zig");
const tui = @import("tui.zig");

pub const RunOutput = struct {
    stdout: []const u8 = "",
    stderr: []const u8 = "",
    exit_code: i32 = 0,
};

const OutputFormat = enum { plain, json };

const GlobalOptions = struct {
    requested_output: ?OutputFormat = null,
    color: render.ColorMode = .auto,
    data_dir: ?[]const u8 = null,
    command_args: []const []const u8 = &.{},
    help: bool = false,
    version: bool = false,

    fn wantsJson(self: GlobalOptions, cfg: config.AppConfig) bool {
        return self.requested_output == .json or (self.requested_output == null and cfg.default_json_output);
    }
};

const Context = struct {
    allocator: std.mem.Allocator,
    globals: GlobalOptions,
    store: storage_mod.JsonFileStorage,
    config_store: config.Store,
    cfg: config.AppConfig,
    today: []const u8,
    runtime: render.RenderOptions,

    fn wantsJson(self: Context) bool {
        return self.globals.wantsJson(self.cfg);
    }
};

const CommandResult = union(enum) {
    ok: []const u8,
    err: errors.ErrorReport,
};

pub fn run(allocator: std.mem.Allocator, args: []const []const u8) !RunOutput {
    const globals = try parseGlobalOptions(allocator, args);
    if (globals.version) return .{ .stdout = "kelp 1.0.0\n" };
    if (globals.help) return .{ .stdout = try topLevelHelp(allocator) };

    const explicit_data_dir = globals.data_dir != null;
    const store = if (globals.data_dir) |data_dir| try storage_mod.JsonFileStorage.at(allocator, data_dir) else try storage_mod.JsonFileStorage.fromEnv(allocator);
    const colocate = explicit_data_dir or envExists("KELP_DATA_DIR");
    const config_store = try config.Store.fromEnvWithDataRoot(allocator, store.rootDir(), colocate);
    const cfg = config_store.load() catch config.AppConfig{};
    const today = try dates.todayUtc().formatAlloc(allocator);
    const ctx = Context{
        .allocator = allocator,
        .globals = globals,
        .store = store,
        .config_store = config_store,
        .cfg = cfg,
        .today = today,
        .runtime = .{ .color = globals.color },
    };

    if (globals.command_args.len == 0) {
        try tui.run(allocator, store, config_store, today);
        return .{};
    }

    const result = execute(ctx) catch |err| CommandResult{ .err = errors.storage(@errorName(err)) };
    switch (result) {
        .ok => |stdout| return .{ .stdout = try ensureTrailingNewline(allocator, stdout) },
        .err => |report| {
            const stderr = if (ctx.wantsJson()) try json.errorEnvelope(allocator, report) else try json.errorPlain(allocator, report);
            return .{ .stderr = stderr, .exit_code = report.exit_code };
        },
    }
}

fn execute(ctx: Context) !CommandResult {
    const args = ctx.globals.command_args;
    if (hasHelp(args)) return .{ .ok = try commandHelp(ctx.allocator, args) };
    const command = args[0];
    if (std.mem.eql(u8, command, "init")) return executeInit(ctx);
    if (std.mem.eql(u8, command, "config")) return executeConfig(ctx, args[1..]);
    if (std.mem.eql(u8, command, "import")) return executeImport(ctx, args[1..]);
    if (std.mem.eql(u8, command, "storage")) return executeStorage(ctx, args[1..]);
    if (std.mem.eql(u8, command, "task")) return executeTask(ctx, args[1..]);
    if (std.mem.eql(u8, command, "project")) return executeProject(ctx, args[1..]);
    if (std.mem.eql(u8, command, "today")) return executeToday(ctx);
    if (std.mem.eql(u8, command, "upcoming")) return executeUpcoming(ctx, args[1..]);
    if (std.mem.eql(u8, command, "review")) return executeReview(ctx, args[1..]);
    if (std.mem.eql(u8, command, "search") or std.mem.eql(u8, command, "find")) return executeSearch(ctx, args[1..]);
    if (std.mem.eql(u8, command, "completions") or std.mem.eql(u8, command, "completion")) return executeCompletions(ctx, args[1..]);
    return .{ .err = errors.usage("usage_error", "unknown command") };
}

fn executeInit(ctx: Context) !CommandResult {
    const path = try ctx.store.init();
    _ = ctx.config_store.load() catch config.AppConfig{};
    if (ctx.wantsJson()) {
        return jsonData(ctx, "init", try std.fmt.allocPrint(ctx.allocator, "{{\"path\": \"{s}\"}}", .{path}));
    }
    return .{ .ok = try render.init(ctx.allocator, ctx.runtime, path) };
}

fn executeConfig(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("usage_error", "missing config command") };
    if (std.mem.eql(u8, args[0], "show")) {
        const cfg = try ctx.config_store.load();
        return configResponse(ctx, "config.show", cfg);
    }
    if (std.mem.eql(u8, args[0], "set")) {
        var cfg = try ctx.config_store.load();
        var changed: std.ArrayList([]const u8) = .empty;
        var i: usize = 1;
        while (i < args.len) : (i += 1) {
            const arg = args[i];
            if (std.mem.eql(u8, arg, "--upcoming-days")) {
                i += 1;
                if (i >= args.len) return .{ .err = errors.usage("missing_value", "--upcoming-days requires a value") };
                cfg.default_upcoming_days = std.fmt.parseInt(i64, args[i], 10) catch return .{ .err = errors.usage("invalid_upcoming_days", "--upcoming-days must be at least 1") };
                if (cfg.default_upcoming_days < 1) return .{ .err = errors.usage("invalid_upcoming_days", "--upcoming-days must be at least 1") };
                try changed.append(ctx.allocator, try std.fmt.allocPrint(ctx.allocator, "default upcoming days -> {d}", .{cfg.default_upcoming_days}));
            } else if (std.mem.eql(u8, arg, "--task-sort")) {
                i += 1;
                if (i >= args.len) return .{ .err = errors.usage("missing_value", "--task-sort requires a value") };
                cfg.default_task_sort = config.parseSortKey(args[i]) orelse return .{ .err = errors.usage("invalid_task_sort", "invalid task sort") };
                try changed.append(ctx.allocator, try std.fmt.allocPrint(ctx.allocator, "default task sort -> {s}", .{cfg.default_task_sort.label()}));
            } else if (std.mem.eql(u8, arg, "--json-output")) {
                cfg.default_json_output = true;
                try changed.append(ctx.allocator, "default json output -> true");
            } else if (std.mem.eql(u8, arg, "--plain-output")) {
                cfg.default_json_output = false;
                try changed.append(ctx.allocator, "default json output -> false");
            } else return .{ .err = errors.usage("usage_error", "unknown config option") };
        }
        if (changed.items.len == 0) return .{ .err = errors.usage("missing_config_changes", "no config changes were provided") };
        try ctx.config_store.save(cfg);
        if (ctx.wantsJson()) return configResponse(ctx, "config.set", cfg);
        return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Config updated", try joinLines(ctx.allocator, changed.items)) };
    }
    return .{ .err = errors.usage("usage_error", "unknown config command") };
}

fn configResponse(ctx: Context, command: []const u8, cfg: config.AppConfig) !CommandResult {
    const path = try ctx.config_store.configFile();
    if (ctx.wantsJson()) {
        var data: std.ArrayList(u8) = .empty;
        try data.print(ctx.allocator,
            \\{{
            \\  "path": 
        , .{});
        try json.appendEscaped(&data, ctx.allocator, path);
        try data.print(ctx.allocator,
            \\,
            \\  "schema_version": {d},
            \\  "default_upcoming_days": {d},
            \\  "default_task_sort": "{s}",
            \\  "default_json_output": {}
            \\}}
        , .{ cfg.schema_version, cfg.default_upcoming_days, cfg.default_task_sort.label(), cfg.default_json_output });
        return jsonData(ctx, command, try data.toOwnedSlice(ctx.allocator));
    }
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Config", try std.fmt.allocPrint(ctx.allocator, "file: {s}\ndefault upcoming days: {d}\ndefault task sort: {s}\ndefault json output: {}", .{ path, cfg.default_upcoming_days, cfg.default_task_sort.label(), cfg.default_json_output })) };
}

fn executeImport(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "legacy")) return .{ .err = errors.usage("usage_error", "missing import command") };
    const source = flagValue(args[1..], "--source") orelse ".";
    var state = try ctx.store.load();
    const summary = legacy.importLegacyFromPath(ctx.allocator, &state, source, ctx.today) catch |err| return .{ .err = errors.storage(@errorName(err)) };
    try ctx.store.save(&state);
    if (ctx.wantsJson()) {
        var data: std.ArrayList(u8) = .empty;
        try data.print(ctx.allocator,
            \\{{
            \\  "imported_tasks": {d},
            \\  "imported_projects": {d},
            \\  "reused_projects": {d},
            \\  "skipped_duplicates": {d},
            \\  "scanned_files": {d},
            \\  "warnings": [
        , .{ summary.imported_tasks, summary.imported_projects, summary.reused_projects, summary.skipped_duplicates, summary.scanned_files });
        for (summary.warnings, 0..) |warning, index| {
            if (index > 0) try data.appendSlice(ctx.allocator, ", ");
            try json.appendEscaped(&data, ctx.allocator, warning);
        }
        try data.appendSlice(ctx.allocator, "]\n}");
        return jsonData(ctx, "import.legacy", try data.toOwnedSlice(ctx.allocator));
    }
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Legacy import complete", try std.fmt.allocPrint(ctx.allocator, "source: {s}\nimported tasks: {d}\nimported projects: {d}\nreused projects: {d}\nskipped duplicates: {d}\nscanned files: {d}", .{ source, summary.imported_tasks, summary.imported_projects, summary.reused_projects, summary.skipped_duplicates, summary.scanned_files })) };
}

fn executeStorage(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("usage_error", "missing storage command") };
    if (std.mem.eql(u8, args[0], "path")) {
        if (ctx.wantsJson()) {
            return jsonData(ctx, "storage.path", try std.fmt.allocPrint(ctx.allocator,
                \\{{"backend": "json", "root_dir": "{s}", "data_file": "{s}", "backup_dir": "{s}", "lock_file": "{s}"}}
            , .{ ctx.store.rootDir(), try ctx.store.dataFile(), try ctx.store.backupDir(), try ctx.store.lockFile() }));
        }
        return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Storage paths", try std.fmt.allocPrint(ctx.allocator, "backend: json\nroot: {s}\ndata: {s}\nbackups: {s}\nlock: {s}", .{ ctx.store.rootDir(), try ctx.store.dataFile(), try ctx.store.backupDir(), try ctx.store.lockFile() })) };
    }
    if (std.mem.eql(u8, args[0], "export")) {
        const output = flagValue(args[1..], "--file") orelse return .{ .err = errors.usage("missing_export_file", "--file is required") };
        const path = try ctx.store.exportTo(output);
        return pathResponse(ctx, "storage.export", "Storage exported", path);
    }
    if (std.mem.eql(u8, args[0], "backup")) {
        const path = try ctx.store.createBackupSnapshot();
        return pathResponse(ctx, "storage.backup", "Backup created", path);
    }
    return .{ .err = errors.usage("usage_error", "unknown storage command") };
}

fn pathResponse(ctx: Context, command: []const u8, title: []const u8, path: []const u8) !CommandResult {
    if (ctx.wantsJson()) {
        var data: std.ArrayList(u8) = .empty;
        try data.appendSlice(ctx.allocator, "{\"path\": ");
        try json.appendEscaped(&data, ctx.allocator, path);
        try data.appendSlice(ctx.allocator, "}");
        return jsonData(ctx, command, try data.toOwnedSlice(ctx.allocator));
    }
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, title, try std.fmt.allocPrint(ctx.allocator, "wrote {s}", .{path})) };
}

fn executeTask(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("usage_error", "missing task command") };
    const sub = args[0];
    if (std.mem.eql(u8, sub, "add") or std.mem.eql(u8, sub, "create")) return taskAdd(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "list") or std.mem.eql(u8, sub, "ls")) return taskList(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "ready")) return taskReady(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "show")) return taskShow(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "edit")) return taskEdit(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "bulk-edit")) return taskBulkEdit(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "next") or std.mem.eql(u8, sub, "na")) return taskStatus(ctx, args[1..], .next_action, "task.next", "Task marked as next action");
    if (std.mem.eql(u8, sub, "start") or std.mem.eql(u8, sub, "begin")) return taskStatus(ctx, args[1..], .in_progress, "task.start", "Task started");
    if (std.mem.eql(u8, sub, "wait") or std.mem.eql(u8, sub, "hold")) return taskWait(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "block") or std.mem.eql(u8, sub, "stuck")) return taskBlock(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "done") or std.mem.eql(u8, sub, "complete")) return taskStatus(ctx, args[1..], .done, "task.done", "Task completed");
    if (std.mem.eql(u8, sub, "reopen")) return taskStatus(ctx, args[1..], .todo, "task.reopen", "Task reopened");
    if (std.mem.eql(u8, sub, "defer") or std.mem.eql(u8, sub, "snooze")) return taskDefer(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "archive")) return taskStatus(ctx, args[1..], .archived, "task.archive", "Task archived");
    if (std.mem.eql(u8, sub, "unarchive")) return taskStatus(ctx, args[1..], .todo, "task.unarchive", "Task unarchived");
    if (std.mem.eql(u8, sub, "delete") or std.mem.eql(u8, sub, "rm")) return taskDelete(ctx, args[1..]);
    return .{ .err = errors.usage("usage_error", "unknown task command") };
}

fn taskAdd(ctx: Context, args: []const []const u8) !CommandResult {
    const title = flagValue(args, "--title") orelse return .{ .err = errors.usage("empty_field", "task title cannot be empty") };
    var state = try ctx.store.load();
    const project_id = if (flagValue(args, "--project")) |project_ref| state.resolveProjectId(project_ref) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "project reference does not exist") } else null;
    const priority = if (flagValue(args, "--priority")) |p| domain.parsePriority(p) orelse return .{ .err = errors.usage("invalid_priority", "invalid priority") } else domain.Priority.medium;
    const due = if (flagValue(args, "--due")) |value| try resolveDateAlloc(ctx, value) else null;
    const wait_until = if (flagValue(args, "--wait-until")) |value| try resolveDateAlloc(ctx, value) else null;
    const repeat = if (flagValue(args, "--repeat")) |value| domain.parseRecurrence(value) orelse return .{ .err = errors.usage("invalid_recurrence", "invalid recurrence") } else null;
    const notes = try readOptionalText(ctx.allocator, flagValue(args, "--notes"), flagValue(args, "--notes-file"));
    const tags = try collectStringValues(ctx.allocator, args, "--tag");
    const deps = try collectU64Values(ctx.allocator, args, "--depends-on");
    const blocked_reason = flagValue(args, "--blocked-reason");
    const task = state.createTask(.{
        .title = title,
        .notes = notes,
        .project_id = project_id,
        .priority = priority,
        .tags = tags,
        .due_date = due,
        .recurrence = repeat,
        .waiting_until = wait_until,
        .blocked_reason = blocked_reason,
        .depends_on = deps,
    }, ctx.today) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "failed to create task") };
    try ctx.store.save(&state);
    if (ctx.wantsJson()) return jsonData(ctx, "task.add", try json.taskView(ctx.allocator, &state, task));
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Task created", try render.taskDetail(ctx.allocator, ctx.runtime, task, &state)) };
}

fn taskList(ctx: Context, args: []const []const u8) !CommandResult {
    const state = try ctx.store.load();
    var tasks = try filterTasks(ctx.allocator, &state, args, ctx.today);
    const sort_key = if (flagValue(args, "--sort")) |value| config.parseSortKey(value) orelse ctx.cfg.default_task_sort else ctx.cfg.default_task_sort;
    sortTasks(tasks.items, sort_key);
    if (flagValue(args, "--limit")) |value| {
        const limit = std.fmt.parseInt(usize, value, 10) catch tasks.items.len;
        if (tasks.items.len > limit) tasks.shrinkRetainingCapacity(limit);
    }
    if (ctx.wantsJson()) return taskListJson(ctx, "task.list", &state, tasks.items);
    return .{ .ok = try render.taskList(ctx.allocator, ctx.runtime, "Tasks", tasks.items, &state) };
}

fn taskReady(ctx: Context, args: []const []const u8) !CommandResult {
    const state = try ctx.store.load();
    var tasks: std.ArrayList(*const domain.Task) = .empty;
    for (state.tasks.items) |*task| {
        if (isReadyTask(&state, task.*)) try tasks.append(ctx.allocator, task);
    }
    sortReadyTasks(tasks.items);
    if (flagValue(args, "--limit")) |value| {
        const limit = std.fmt.parseInt(usize, value, 10) catch tasks.items.len;
        if (tasks.items.len > limit) tasks.shrinkRetainingCapacity(limit);
    }
    if (ctx.wantsJson()) return taskListJson(ctx, "task.ready", &state, tasks.items);
    return .{ .ok = try render.taskList(ctx.allocator, ctx.runtime, "Ready tasks", tasks.items, &state) };
}

fn taskShow(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("missing_task_id", "task id is required") };
    const id = parseId(args[0]) orelse return .{ .err = errors.usage("invalid_task_id", "invalid task id") };
    const state = try ctx.store.load();
    const task = state.findTask(id) orelse return .{ .err = errors.notFound("task_not_found", try std.fmt.allocPrint(ctx.allocator, "task {d} does not exist", .{id})) };
    if (ctx.wantsJson()) return jsonData(ctx, "task.show", try json.taskView(ctx.allocator, &state, task.*));
    return .{ .ok = try render.taskDetail(ctx.allocator, ctx.runtime, task.*, &state) };
}

fn taskEdit(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("missing_task_id", "task id is required") };
    const id = parseId(args[0]) orelse return .{ .err = errors.usage("invalid_task_id", "invalid task id") };
    var state = try ctx.store.load();
    var patch = domain.TaskPatch{};
    if (flagValue(args[1..], "--title")) |value| patch.title = value;
    if (flagValue(args[1..], "--notes")) |value| patch.notes = .{ .set = value };
    if (flagValue(args[1..], "--notes-file")) |path| patch.notes = .{ .set = try readFileText(ctx.allocator, path) };
    if (hasFlag(args[1..], "--clear-notes")) patch.notes = .clear;
    if (flagValue(args[1..], "--project")) |value| patch.project_id = .{ .set = state.resolveProjectId(value) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "project not found") } };
    if (hasFlag(args[1..], "--clear-project")) patch.project_id = .clear;
    if (flagValue(args[1..], "--status")) |value| patch.status = domain.parseStatus(value) orelse return .{ .err = errors.usage("invalid_status", "invalid status") };
    if (flagValue(args[1..], "--priority")) |value| patch.priority = domain.parsePriority(value) orelse return .{ .err = errors.usage("invalid_priority", "invalid priority") };
    const tags = try collectStringValues(ctx.allocator, args[1..], "--tag");
    if (tags.len > 0) patch.tags = tags;
    if (hasFlag(args[1..], "--clear-tags")) patch.tags = &.{};
    if (flagValue(args[1..], "--due")) |value| patch.due_date = .{ .set = try resolveDateAlloc(ctx, value) };
    if (hasFlag(args[1..], "--clear-due")) patch.due_date = .clear;
    if (flagValue(args[1..], "--repeat")) |value| patch.recurrence = .{ .set = domain.parseRecurrence(value) orelse return .{ .err = errors.usage("invalid_recurrence", "invalid recurrence") } };
    if (hasFlag(args[1..], "--clear-repeat")) patch.recurrence = .clear;
    if (flagValue(args[1..], "--wait-until")) |value| patch.waiting_until = .{ .set = try resolveDateAlloc(ctx, value) };
    if (hasFlag(args[1..], "--clear-wait-until")) patch.waiting_until = .clear;
    if (flagValue(args[1..], "--blocked-reason")) |value| patch.blocked_reason = .{ .set = value };
    if (hasFlag(args[1..], "--clear-blocked-reason")) patch.blocked_reason = .clear;
    const deps = try collectU64Values(ctx.allocator, args[1..], "--depends-on");
    if (deps.len > 0) patch.depends_on = deps;
    if (hasFlag(args[1..], "--clear-depends-on")) patch.depends_on = &.{};
    if (patch.isEmpty()) return .{ .err = errors.usage("missing_task_changes", "no task changes were provided") };
    var spawned: ?u64 = null;
    if (patch.status) |status| {
        patch.status = null;
        try state.applyTaskPatch(id, patch, ctx.today);
        spawned = state.setTaskStatus(id, status, ctx.today) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "failed to update task") };
    } else try state.applyTaskPatch(id, patch, ctx.today);
    try ctx.store.save(&state);
    const task = state.findTask(id).?;
    if (ctx.wantsJson()) return taskMutationJson(ctx, "task.edit", &state, task.*, spawned);
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Task updated", try render.taskDetail(ctx.allocator, ctx.runtime, task.*, &state)) };
}

fn taskBulkEdit(ctx: Context, args: []const []const u8) !CommandResult {
    var ids: std.ArrayList(u64) = .empty;
    var i: usize = 0;
    while (i < args.len and !std.mem.startsWith(u8, args[i], "--")) : (i += 1) {
        if (parseId(args[i])) |id| try ids.append(ctx.allocator, id);
    }
    if (ids.items.len == 0) return .{ .err = errors.usage("missing_task_ids", "bulk edit requires at least one task id") };
    var state = try ctx.store.load();
    var patch = domain.TaskPatch{};
    if (flagValue(args[i..], "--project")) |value| patch.project_id = .{ .set = state.resolveProjectId(value) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "project not found") } };
    if (hasFlag(args[i..], "--clear-project")) patch.project_id = .clear;
    if (flagValue(args[i..], "--status")) |value| patch.status = domain.parseStatus(value) orelse return .{ .err = errors.usage("invalid_status", "invalid status") };
    if (flagValue(args[i..], "--priority")) |value| patch.priority = domain.parsePriority(value) orelse return .{ .err = errors.usage("invalid_priority", "invalid priority") };
    const tags = try collectStringValues(ctx.allocator, args[i..], "--tag");
    if (tags.len > 0) patch.tags = tags;
    if (hasFlag(args[i..], "--clear-tags")) patch.tags = &.{};
    if (flagValue(args[i..], "--due")) |value| patch.due_date = .{ .set = try resolveDateAlloc(ctx, value) };
    if (hasFlag(args[i..], "--clear-due")) patch.due_date = .clear;
    if (flagValue(args[i..], "--repeat")) |value| patch.recurrence = .{ .set = domain.parseRecurrence(value) orelse return .{ .err = errors.usage("invalid_recurrence", "invalid recurrence") } };
    if (hasFlag(args[i..], "--clear-repeat")) patch.recurrence = .clear;
    if (patch.isEmpty()) return .{ .err = errors.usage("missing_bulk_task_changes", "no bulk edit changes were provided") };
    var spawned: std.ArrayList(u64) = .empty;
    const desired_status = patch.status;
    patch.status = null;
    for (ids.items) |id| {
        if (!patch.isEmpty()) try state.applyTaskPatch(id, patch, ctx.today);
        if (desired_status) |status| {
            if (try state.setTaskStatus(id, status, ctx.today)) |new_id| try spawned.append(ctx.allocator, new_id);
        }
    }
    try ctx.store.save(&state);
    if (ctx.wantsJson()) {
        var data: std.ArrayList(u8) = .empty;
        try data.print(ctx.allocator, "{{\"updated_tasks\": {d}, \"spawned_task_ids\": [", .{ids.items.len});
        for (spawned.items, 0..) |id, idx| {
            if (idx > 0) try data.appendSlice(ctx.allocator, ", ");
            try data.print(ctx.allocator, "{d}", .{id});
        }
        try data.appendSlice(ctx.allocator, "]}");
        return jsonData(ctx, "task.bulk_edit", try data.toOwnedSlice(ctx.allocator));
    }
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Bulk edit applied", try std.fmt.allocPrint(ctx.allocator, "updated tasks: {d}", .{ids.items.len})) };
}

fn taskStatus(ctx: Context, args: []const []const u8, status: domain.TaskStatus, command: []const u8, title: []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("missing_task_id", "task id is required") };
    const id = parseId(args[0]) orelse return .{ .err = errors.usage("invalid_task_id", "invalid task id") };
    var state = try ctx.store.load();
    const spawned = state.setTaskStatus(id, status, ctx.today) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "failed to update task") };
    try ctx.store.save(&state);
    const task = state.findTask(id).?;
    if (ctx.wantsJson()) return taskMutationJson(ctx, command, &state, task.*, spawned);
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, title, try render.taskDetail(ctx.allocator, ctx.runtime, task.*, &state)) };
}

fn taskWait(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("missing_task_id", "task id is required") };
    const id = parseId(args[0]) orelse return .{ .err = errors.usage("invalid_task_id", "invalid task id") };
    var state = try ctx.store.load();
    _ = state.setTaskStatus(id, .waiting, ctx.today) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "failed to update task") };
    if (flagValue(args[1..], "--until")) |value| try state.applyTaskPatch(id, .{ .waiting_until = .{ .set = try resolveDateAlloc(ctx, value) } }, ctx.today);
    try ctx.store.save(&state);
    const task = state.findTask(id).?;
    if (ctx.wantsJson()) return jsonData(ctx, "task.wait", try json.taskView(ctx.allocator, &state, task.*));
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Task marked as waiting", try render.taskDetail(ctx.allocator, ctx.runtime, task.*, &state)) };
}

fn taskBlock(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("missing_task_id", "task id is required") };
    const id = parseId(args[0]) orelse return .{ .err = errors.usage("invalid_task_id", "invalid task id") };
    var state = try ctx.store.load();
    _ = state.setTaskStatus(id, .blocked, ctx.today) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "failed to update task") };
    if (flagValue(args[1..], "--reason")) |value| try state.applyTaskPatch(id, .{ .blocked_reason = .{ .set = value } }, ctx.today);
    try ctx.store.save(&state);
    const task = state.findTask(id).?;
    if (ctx.wantsJson()) return jsonData(ctx, "task.block", try json.taskView(ctx.allocator, &state, task.*));
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Task marked as blocked", try render.taskDetail(ctx.allocator, ctx.runtime, task.*, &state)) };
}

fn taskDefer(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("missing_task_id", "task id is required") };
    const id = parseId(args[0]) orelse return .{ .err = errors.usage("invalid_task_id", "invalid task id") };
    const due = if (flagValue(args[1..], "--until")) |value| try resolveDateAlloc(ctx, value) else if (flagValue(args[1..], "--days")) |days_raw| blk: {
        const days = std.fmt.parseInt(i64, days_raw, 10) catch return .{ .err = errors.usage("invalid_defer_days", "--days must be greater than 0") };
        if (days <= 0) return .{ .err = errors.usage("invalid_defer_days", "--days must be greater than 0") };
        const today = try dates.Date.parse(ctx.today);
        break :blk try today.addDays(days).formatAlloc(ctx.allocator);
    } else return .{ .err = errors.usage("missing_defer_date", "provide either --until YYYY-MM-DD or --days N") };
    var state = try ctx.store.load();
    try state.applyTaskPatch(id, .{ .due_date = .{ .set = due } }, ctx.today);
    try ctx.store.save(&state);
    const task = state.findTask(id).?;
    if (ctx.wantsJson()) return jsonData(ctx, "task.defer", try json.taskView(ctx.allocator, &state, task.*));
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Task deferred", try render.taskDetail(ctx.allocator, ctx.runtime, task.*, &state)) };
}

fn taskDelete(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("missing_task_id", "task id is required") };
    const id = parseId(args[0]) orelse return .{ .err = errors.usage("invalid_task_id", "invalid task id") };
    var state = try ctx.store.load();
    const removed = state.deleteTask(id) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "task not found") };
    try ctx.store.save(&state);
    if (ctx.wantsJson()) return jsonData(ctx, "task.delete", try std.fmt.allocPrint(ctx.allocator, "{{\"removed_id\": {d}, \"removed_title\": \"{s}\"}}", .{ removed.id, removed.title }));
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Task deleted", try std.fmt.allocPrint(ctx.allocator, "removed task {d}: {s}", .{ removed.id, removed.title })) };
}

fn taskListJson(ctx: Context, command: []const u8, state: *const domain.AppState, tasks: []const *const domain.Task) !CommandResult {
    var data: std.ArrayList(u8) = .empty;
    try data.appendSlice(ctx.allocator, "{\"tasks\": [");
    for (tasks, 0..) |task, index| {
        if (index > 0) try data.appendSlice(ctx.allocator, ", ");
        try data.appendSlice(ctx.allocator, try json.taskView(ctx.allocator, state, task.*));
    }
    try data.appendSlice(ctx.allocator, "]}");
    return jsonData(ctx, command, try data.toOwnedSlice(ctx.allocator));
}

fn taskMutationJson(ctx: Context, command: []const u8, state: *const domain.AppState, task: domain.Task, spawned: ?u64) !CommandResult {
    var data: std.ArrayList(u8) = .empty;
    try data.appendSlice(ctx.allocator, "{\"task\": ");
    try data.appendSlice(ctx.allocator, try json.taskView(ctx.allocator, state, task));
    try data.appendSlice(ctx.allocator, ", \"spawned_task_id\": ");
    if (spawned) |id| try data.print(ctx.allocator, "{d}", .{id}) else try data.appendSlice(ctx.allocator, "null");
    try data.appendSlice(ctx.allocator, "}");
    return jsonData(ctx, command, try data.toOwnedSlice(ctx.allocator));
}

fn executeProject(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("usage_error", "missing project command") };
    const sub = args[0];
    if (std.mem.eql(u8, sub, "add") or std.mem.eql(u8, sub, "create")) return projectAdd(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "list") or std.mem.eql(u8, sub, "ls")) return projectList(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "show")) return projectShow(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "edit")) return projectEdit(ctx, args[1..]);
    if (std.mem.eql(u8, sub, "archive")) return projectStatus(ctx, args[1..], true);
    if (std.mem.eql(u8, sub, "unarchive")) return projectStatus(ctx, args[1..], false);
    return .{ .err = errors.usage("usage_error", "unknown project command") };
}

fn projectAdd(ctx: Context, args: []const []const u8) !CommandResult {
    const name = flagValue(args, "--name") orelse return .{ .err = errors.usage("empty_field", "project name cannot be empty") };
    var state = try ctx.store.load();
    const description = try readOptionalText(ctx.allocator, flagValue(args, "--description"), flagValue(args, "--description-file"));
    const deadline = if (flagValue(args, "--deadline")) |value| try resolveDateAlloc(ctx, value) else null;
    const project = state.createProject(name, description, deadline, ctx.today) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "failed to create project") };
    try ctx.store.save(&state);
    const summary = try state.projectSummary(project.id, ctx.today);
    if (ctx.wantsJson()) return jsonData(ctx, "project.add", try json.projectView(ctx.allocator, project, summary));
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Project created", try std.fmt.allocPrint(ctx.allocator, "created project {d}: {s}", .{ project.id, project.name })) };
}

fn projectList(ctx: Context, args: []const []const u8) !CommandResult {
    const state = try ctx.store.load();
    var entries: std.ArrayList(render.ProjectEntry) = .empty;
    const archived = hasFlag(args, "--archived");
    const today = try dates.Date.parse(ctx.today);
    for (state.projects.items) |project| {
        if (archived and project.status != .archived) continue;
        if (!archived and project.status != .active) continue;
        const summary = try state.projectSummary(project.id, ctx.today);
        if (hasFlag(args, "--at-risk") and !(summary.overdue_tasks > 0 or summary.blocked_tasks > 0 or summary.dependency_blocked_tasks > 0 or (summary.open_tasks > 0 and summary.next_action_tasks == 0))) continue;
        if (hasFlag(args, "--missing-next-action") and !(summary.open_tasks > 0 and summary.next_action_tasks == 0)) continue;
        if (flagValue(args, "--deadline-within")) |raw| {
            const days = std.fmt.parseInt(i64, raw, 10) catch return .{ .err = errors.usage("invalid_deadline_window", "--deadline-within must be at least 1") };
            if (days < 1) return .{ .err = errors.usage("invalid_deadline_window", "--deadline-within must be at least 1") };
            const deadline = if (project.deadline) |value| dates.Date.parse(value) catch null else null;
            if (deadline == null or deadline.?.compare(today) == .lt or deadline.?.compare(today.addDays(days)) == .gt) continue;
        }
        try entries.append(ctx.allocator, .{ .project = project, .summary = summary });
    }
    std.mem.sort(render.ProjectEntry, entries.items, {}, lessProjectEntry);
    if (flagValue(args, "--limit")) |value| {
        const limit = std.fmt.parseInt(usize, value, 10) catch entries.items.len;
        if (entries.items.len > limit) entries.shrinkRetainingCapacity(limit);
    }
    if (ctx.wantsJson()) return projectListJson(ctx, "project.list", entries.items);
    return .{ .ok = try render.projectList(ctx.allocator, ctx.runtime, "Projects", entries.items) };
}

fn projectShow(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("missing_project", "project reference is required") };
    const state = try ctx.store.load();
    const id = state.resolveProjectId(args[0]) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "project not found") };
    const project = state.findProject(id).?;
    const summary = try state.projectSummary(project.id, ctx.today);
    var tasks: std.ArrayList(*const domain.Task) = .empty;
    for (state.tasks.items) |*task| {
        if (task.project_id == id and task.status != .archived) try tasks.append(ctx.allocator, task);
    }
    sortTasks(tasks.items, .due);
    if (ctx.wantsJson()) {
        var data: std.ArrayList(u8) = .empty;
        try data.appendSlice(ctx.allocator, "{\"project\": ");
        try data.appendSlice(ctx.allocator, try json.projectView(ctx.allocator, project.*, summary));
        try data.appendSlice(ctx.allocator, ", \"tasks\": [");
        for (tasks.items, 0..) |task, index| {
            if (index > 0) try data.appendSlice(ctx.allocator, ", ");
            try data.appendSlice(ctx.allocator, try json.taskView(ctx.allocator, &state, task.*));
        }
        try data.appendSlice(ctx.allocator, "]}");
        return jsonData(ctx, "project.show", try data.toOwnedSlice(ctx.allocator));
    }
    const project_body = try std.fmt.allocPrint(ctx.allocator, "id: {d}\nstatus: {s}\nprogress: {d}% complete\nopen tasks: {d}\ndeadline: {s}", .{ project.id, project.status.label(), summary.completion_percent, summary.open_tasks, project.deadline orelse "none" });
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, try std.fmt.allocPrint(ctx.allocator, "Project {s}", .{project.name}), project_body) };
}

fn projectEdit(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("missing_project", "project reference is required") };
    var state = try ctx.store.load();
    const id = state.resolveProjectId(args[0]) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "project not found") };
    var patch = domain.ProjectPatch{};
    if (flagValue(args[1..], "--description")) |value| patch.description = .{ .set = value };
    if (flagValue(args[1..], "--description-file")) |path| patch.description = .{ .set = try readFileText(ctx.allocator, path) };
    if (hasFlag(args[1..], "--clear-description")) patch.description = .clear;
    if (flagValue(args[1..], "--deadline")) |value| patch.deadline = .{ .set = try resolveDateAlloc(ctx, value) };
    if (hasFlag(args[1..], "--clear-deadline")) patch.deadline = .clear;
    if (patch.isEmpty()) return .{ .err = errors.usage("missing_project_changes", "no project changes were provided") };
    try state.applyProjectPatch(id, patch, ctx.today);
    try ctx.store.save(&state);
    const project = state.findProject(id).?;
    const summary = try state.projectSummary(project.id, ctx.today);
    if (ctx.wantsJson()) return jsonData(ctx, "project.edit", try json.projectView(ctx.allocator, project.*, summary));
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, "Project updated", try std.fmt.allocPrint(ctx.allocator, "project {d}: {s}\ndeadline: {s}\ndescription: {s}", .{ project.id, project.name, project.deadline orelse "none", project.description orelse "none" })) };
}

fn projectStatus(ctx: Context, args: []const []const u8, archive: bool) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("missing_project", "project reference is required") };
    var state = try ctx.store.load();
    const id = state.resolveProjectId(args[0]) catch |err| return .{ .err = errors.fromDomain(ctx.allocator, err, "project not found") };
    if (archive) try state.archiveProject(id, ctx.today) else try state.activateProject(id, ctx.today);
    try ctx.store.save(&state);
    const project = state.findProject(id).?;
    const summary = try state.projectSummary(project.id, ctx.today);
    if (ctx.wantsJson()) return jsonData(ctx, if (archive) "project.archive" else "project.unarchive", try json.projectView(ctx.allocator, project.*, summary));
    return .{ .ok = try render.confirmation(ctx.allocator, ctx.runtime, if (archive) "Project archived" else "Project reactivated", try std.fmt.allocPrint(ctx.allocator, "{s} project {d}: {s}", .{ if (archive) "archived" else "reactivated", project.id, project.name })) };
}

fn projectListJson(ctx: Context, command: []const u8, entries: []const render.ProjectEntry) !CommandResult {
    var data: std.ArrayList(u8) = .empty;
    try data.appendSlice(ctx.allocator, "{\"projects\": [");
    for (entries, 0..) |entry, index| {
        if (index > 0) try data.appendSlice(ctx.allocator, ", ");
        try data.appendSlice(ctx.allocator, try json.projectView(ctx.allocator, entry.project, entry.summary));
    }
    try data.appendSlice(ctx.allocator, "]}");
    return jsonData(ctx, command, try data.toOwnedSlice(ctx.allocator));
}

fn executeToday(ctx: Context) !CommandResult {
    const state = try ctx.store.load();
    const sections = try reviewSections(ctx.allocator, &state, ctx.today, .today);
    if (ctx.wantsJson()) return sectionsJson(ctx, "today", &state, sections);
    return .{ .ok = try renderSectionsPlain(ctx, "Today", &state, sections) };
}

fn executeUpcoming(ctx: Context, args: []const []const u8) !CommandResult {
    const days = if (flagValue(args, "--days")) |raw| std.fmt.parseInt(i64, raw, 10) catch return .{ .err = errors.usage("invalid_upcoming_days", "--days must be at least 1") } else ctx.cfg.default_upcoming_days;
    if (days < 1) return .{ .err = errors.usage("invalid_upcoming_days", "--days must be at least 1") };
    const state = try ctx.store.load();
    const today = try dates.Date.parse(ctx.today);
    const end = today.addDays(days);
    var tasks: std.ArrayList(*const domain.Task) = .empty;
    for (state.tasks.items) |*task| {
        if (!task.isOpen() or !state.taskInActiveProject(task.*) or task.due_date == null) continue;
        const due = dates.Date.parse(task.due_date.?) catch continue;
        if (due.compare(today) == .gt and due.compare(end) != .gt) try tasks.append(ctx.allocator, task);
    }
    sortTasks(tasks.items, .due);
    const sections = try groupByDue(ctx.allocator, tasks.items);
    if (ctx.wantsJson()) return sectionsJson(ctx, "upcoming", &state, sections.items);
    return .{ .ok = try renderSectionsPlain(ctx, "Upcoming", &state, sections.items) };
}

fn executeReview(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("usage_error", "missing review command") };
    if (!std.mem.eql(u8, args[0], "daily") and !std.mem.eql(u8, args[0], "day") and !std.mem.eql(u8, args[0], "weekly") and !std.mem.eql(u8, args[0], "week")) {
        return .{ .err = errors.usage("usage_error", "unknown review command") };
    }
    var state = try ctx.store.load();
    const actions = try applyReviewActions(ctx, &state, args[1..]);
    if (actions.items.len > 0) try ctx.store.save(&state);
    const mode: ReviewMode = if (std.mem.startsWith(u8, args[0], "week")) .weekly else .daily;
    const sections = try reviewSections(ctx.allocator, &state, ctx.today, mode);
    if (ctx.wantsJson()) {
        var data: std.ArrayList(u8) = .empty;
        try data.appendSlice(ctx.allocator, "{\"applied_actions\": [");
        for (actions.items, 0..) |action, index| {
            if (index > 0) try data.appendSlice(ctx.allocator, ", ");
            try json.appendEscaped(&data, ctx.allocator, action);
        }
        try data.appendSlice(ctx.allocator, "], \"sections\": ");
        try appendSectionsJson(&data, ctx, &state, sections);
        if (mode == .weekly) {
            try appendWeeklyProjectArrays(&data, ctx, &state);
        }
        try data.appendSlice(ctx.allocator, "}");
        return jsonData(ctx, if (mode == .weekly) "review.weekly" else "review.daily", try data.toOwnedSlice(ctx.allocator));
    }
    return .{ .ok = try renderSectionsPlain(ctx, if (mode == .weekly) "Weekly review" else "Daily review", &state, sections) };
}

fn executeSearch(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0 or std.mem.trim(u8, args[0], " \t\r\n").len == 0) return .{ .err = errors.usage("empty_search_query", "search query cannot be empty") };
    const query = args[0];
    const state = try ctx.store.load();
    var tasks: std.ArrayList(*const domain.Task) = .empty;
    for (state.tasks.items) |*task| {
        const project_match = if (task.project_id) |id| blk: {
            const name = state.projectName(id) orelse "";
            break :blk containsIgnoreCase(name, query);
        } else false;
        if (task.status != .archived and state.taskInActiveProject(task.*) and (task.matchesQuery(query) or project_match)) try tasks.append(ctx.allocator, task);
    }
    sortTasks(tasks.items, .due);
    var projects: std.ArrayList(render.ProjectEntry) = .empty;
    for (state.projects.items) |project| {
        if (project.status == .active and project.matchesQuery(query)) try projects.append(ctx.allocator, .{ .project = project, .summary = try state.projectSummary(project.id, ctx.today) });
    }
    std.mem.sort(render.ProjectEntry, projects.items, {}, lessProjectEntry);
    if (ctx.wantsJson()) {
        var data: std.ArrayList(u8) = .empty;
        try data.appendSlice(ctx.allocator, "{\"tasks\": [");
        for (tasks.items, 0..) |task, index| {
            if (index > 0) try data.appendSlice(ctx.allocator, ", ");
            try data.appendSlice(ctx.allocator, try json.taskView(ctx.allocator, &state, task.*));
        }
        try data.appendSlice(ctx.allocator, "], \"projects\": [");
        for (projects.items, 0..) |entry, index| {
            if (index > 0) try data.appendSlice(ctx.allocator, ", ");
            try data.appendSlice(ctx.allocator, try json.projectView(ctx.allocator, entry.project, entry.summary));
        }
        try data.appendSlice(ctx.allocator, "]}");
        return jsonData(ctx, "search", try data.toOwnedSlice(ctx.allocator));
    }
    return .{ .ok = try render.taskList(ctx.allocator, ctx.runtime, "Matching tasks", tasks.items, &state) };
}

fn jsonData(ctx: Context, command: []const u8, data: []const u8) !CommandResult {
    return .{ .ok = try json.successEnvelope(ctx.allocator, command, data) };
}

const TaskSection = struct {
    name: []const u8,
    tasks: []const *const domain.Task,
};

const ReviewMode = enum { today, daily, weekly };

fn filterTasks(allocator: std.mem.Allocator, state: *const domain.AppState, args: []const []const u8, today_raw: []const u8) !std.ArrayList(*const domain.Task) {
    const today = try dates.Date.parse(today_raw);
    const project_id = if (flagValue(args, "--project")) |value| state.resolveProjectId(value) catch null else null;
    const status = if (flagValue(args, "--status")) |value| domain.parseStatus(value) else null;
    const priority = if (flagValue(args, "--priority")) |value| domain.parsePriority(value) else null;
    const query = flagValue(args, "--query");
    const include_all = hasFlag(args, "--all");
    const ready_only = hasFlag(args, "--ready");
    const due_today = hasFlag(args, "--due-today");
    const overdue = hasFlag(args, "--overdue");
    const tags = try collectStringValues(allocator, args, "--tag");
    var out: std.ArrayList(*const domain.Task) = .empty;
    for (state.tasks.items) |*task| {
        if (project_id) |id| {
            if (task.project_id != id) continue;
        } else if (!include_all and !state.taskInActiveProject(task.*)) continue;
        if (status) |s| {
            if (task.status != s) continue;
        } else if (!include_all and !task.status.isOpen()) continue;
        if (priority) |p| if (task.priority != p) continue;
        var tags_ok = true;
        for (tags) |tag| {
            if (!task.hasTag(tag)) tags_ok = false;
        }
        if (!tags_ok) continue;
        if (query) |q| if (!task.matchesQuery(q)) continue;
        if (ready_only and !isReadyTask(state, task.*)) continue;
        if (due_today and !dateEquals(task.due_date, today)) continue;
        if (overdue and !dateBefore(task.due_date, today)) continue;
        try out.append(allocator, task);
    }
    return out;
}

fn activeOpenTasks(allocator: std.mem.Allocator, state: *const domain.AppState) !std.ArrayList(*const domain.Task) {
    var out: std.ArrayList(*const domain.Task) = .empty;
    for (state.tasks.items) |*task| {
        if (task.status.isOpen() and state.taskInActiveProject(task.*)) try out.append(allocator, task);
    }
    return out;
}

fn reviewSections(allocator: std.mem.Allocator, state: *const domain.AppState, today_raw: []const u8, mode: ReviewMode) ![]const TaskSection {
    const today = try dates.Date.parse(today_raw);
    const window_end = today.addDays(7);
    const stale_cutoff = today.addDays(-7);
    const active = try activeOpenTasks(allocator, state);
    var sections: std.ArrayList(TaskSection) = .empty;
    try sections.append(allocator, .{ .name = if (mode == .daily) "Carryover" else "Overdue", .tasks = try selectTasks(allocator, active.items, struct {
        fn f(t: *const domain.Task, s: *const domain.AppState, d: dates.Date, _: dates.Date, _: dates.Date) bool {
            _ = s;
            return dateBefore(t.due_date, d);
        }
    }.f, state, today, window_end, stale_cutoff) });
    if (mode != .weekly) {
        try sections.append(allocator, .{ .name = "Due today", .tasks = try selectTasks(allocator, active.items, struct {
            fn f(t: *const domain.Task, s: *const domain.AppState, d: dates.Date, _: dates.Date, _: dates.Date) bool {
                _ = s;
                return dateEquals(t.due_date, d);
            }
        }.f, state, today, window_end, stale_cutoff) });
    }
    try sections.append(allocator, .{ .name = "Next actions", .tasks = try selectTasks(allocator, active.items, struct {
        fn f(t: *const domain.Task, s: *const domain.AppState, _: dates.Date, _: dates.Date, _: dates.Date) bool {
            _ = s;
            return t.status.isNextAction();
        }
    }.f, state, today, window_end, stale_cutoff) });
    if (mode == .today) {
        try sections.append(allocator, .{ .name = "In progress", .tasks = try selectTasks(allocator, active.items, struct {
            fn f(t: *const domain.Task, s: *const domain.AppState, _: dates.Date, _: dates.Date, _: dates.Date) bool {
                _ = s;
                return t.status == .in_progress;
            }
        }.f, state, today, window_end, stale_cutoff) });
    }
    try sections.append(allocator, .{ .name = "Blocked", .tasks = try selectTasks(allocator, active.items, struct {
        fn f(t: *const domain.Task, s: *const domain.AppState, _: dates.Date, _: dates.Date, _: dates.Date) bool {
            _ = s;
            return t.status == .blocked;
        }
    }.f, state, today, window_end, stale_cutoff) });
    if (mode != .today) {
        try sections.append(allocator, .{ .name = "Waiting", .tasks = try selectTasks(allocator, active.items, struct {
            fn f(t: *const domain.Task, s: *const domain.AppState, _: dates.Date, _: dates.Date, _: dates.Date) bool {
                _ = s;
                return t.status == .waiting;
            }
        }.f, state, today, window_end, stale_cutoff) });
    }
    try sections.append(allocator, .{ .name = "Blocked by dependencies", .tasks = try selectTasks(allocator, active.items, struct {
        fn f(t: *const domain.Task, s: *const domain.AppState, _: dates.Date, _: dates.Date, _: dates.Date) bool {
            return s.hasUnresolvedDependencies(t.*);
        }
    }.f, state, today, window_end, stale_cutoff) });
    if (mode == .weekly) {
        try sections.append(allocator, .{ .name = "Due this week", .tasks = try selectTasks(allocator, active.items, struct {
            fn f(t: *const domain.Task, s: *const domain.AppState, d: dates.Date, end: dates.Date, _: dates.Date) bool {
                _ = s;
                const due = if (t.due_date) |value| dates.Date.parse(value) catch return false else return false;
                return due.compare(d) != .lt and due.compare(end) != .gt;
            }
        }.f, state, today, window_end, stale_cutoff) });
    } else {
        try sections.append(allocator, .{ .name = "Waiting follow-up", .tasks = try selectTasks(allocator, active.items, struct {
            fn f(t: *const domain.Task, s: *const domain.AppState, d: dates.Date, _: dates.Date, _: dates.Date) bool {
                _ = s;
                if (t.status != .waiting) return false;
                if (t.waiting_until) |value| {
                    const until = dates.Date.parse(value) catch return true;
                    return until.compare(d) != .gt;
                }
                return true;
            }
        }.f, state, today, window_end, stale_cutoff) });
    }
    if (mode == .weekly) {
        try sections.append(allocator, .{ .name = "Stale tasks", .tasks = try selectTasks(allocator, active.items, struct {
            fn f(t: *const domain.Task, s: *const domain.AppState, _: dates.Date, _: dates.Date, stale: dates.Date) bool {
                _ = s;
                const updated = dates.Date.parse(t.updated_on) catch return false;
                return updated.compare(stale) != .gt;
            }
        }.f, state, today, window_end, stale_cutoff) });
    } else if (mode == .daily) {
        try sections.append(allocator, .{ .name = "Needs scheduling", .tasks = try selectTasks(allocator, active.items, struct {
            fn f(t: *const domain.Task, s: *const domain.AppState, _: dates.Date, _: dates.Date, _: dates.Date) bool {
                _ = s;
                return t.due_date == null and t.priority == .high;
            }
        }.f, state, today, window_end, stale_cutoff) });
    }
    for (sections.items) |section| sortTasks(section.tasks, .due);
    return sections.toOwnedSlice(allocator);
}

fn selectTasks(
    allocator: std.mem.Allocator,
    tasks: []const *const domain.Task,
    pred: *const fn (*const domain.Task, *const domain.AppState, dates.Date, dates.Date, dates.Date) bool,
    state: *const domain.AppState,
    today: dates.Date,
    window_end: dates.Date,
    stale_cutoff: dates.Date,
) ![]const *const domain.Task {
    var out: std.ArrayList(*const domain.Task) = .empty;
    for (tasks) |task| if (pred(task, state, today, window_end, stale_cutoff)) try out.append(allocator, task);
    return out.toOwnedSlice(allocator);
}

fn groupByDue(allocator: std.mem.Allocator, tasks: []const *const domain.Task) !std.ArrayList(TaskSection) {
    var sections: std.ArrayList(TaskSection) = .empty;
    for (tasks) |task| {
        const label = task.due_date orelse "No due date";
        var found: ?usize = null;
        for (sections.items, 0..) |section, index| {
            if (std.mem.eql(u8, section.name, label)) found = index;
        }
        if (found) |index| {
            var list = std.ArrayList(*const domain.Task).fromOwnedSlice(@constCast(sections.items[index].tasks));
            try list.append(allocator, task);
            sections.items[index].tasks = try list.toOwnedSlice(allocator);
        } else {
            var list: std.ArrayList(*const domain.Task) = .empty;
            try list.append(allocator, task);
            try sections.append(allocator, .{ .name = label, .tasks = try list.toOwnedSlice(allocator) });
        }
    }
    return sections;
}

fn sectionsJson(ctx: Context, command: []const u8, state: *const domain.AppState, sections: []const TaskSection) !CommandResult {
    var data: std.ArrayList(u8) = .empty;
    try data.appendSlice(ctx.allocator, "{\"sections\": ");
    try appendSectionsJson(&data, ctx, state, sections);
    try data.appendSlice(ctx.allocator, "}");
    return jsonData(ctx, command, try data.toOwnedSlice(ctx.allocator));
}

fn appendSectionsJson(out: *std.ArrayList(u8), ctx: Context, state: *const domain.AppState, sections: []const TaskSection) !void {
    try out.appendSlice(ctx.allocator, "[");
    for (sections, 0..) |section, section_index| {
        if (section_index > 0) try out.appendSlice(ctx.allocator, ", ");
        try out.appendSlice(ctx.allocator, "{\"name\": ");
        try json.appendEscaped(out, ctx.allocator, section.name);
        try out.appendSlice(ctx.allocator, ", \"tasks\": [");
        for (section.tasks, 0..) |task, task_index| {
            if (task_index > 0) try out.appendSlice(ctx.allocator, ", ");
            try out.appendSlice(ctx.allocator, try json.taskView(ctx.allocator, state, task.*));
        }
        try out.appendSlice(ctx.allocator, "]}");
    }
    try out.appendSlice(ctx.allocator, "]");
}

fn appendWeeklyProjectArrays(out: *std.ArrayList(u8), ctx: Context, state: *const domain.AppState) !void {
    const names = [_][]const u8{ "projects_without_next_actions", "stalled_projects", "projects_missing_deadlines", "deadline_projects", "at_risk_projects" };
    for (names) |name| {
        try out.appendSlice(ctx.allocator, ", ");
        try json.appendEscaped(out, ctx.allocator, name);
        try out.appendSlice(ctx.allocator, ": [");
        var first = true;
        const today = try dates.Date.parse(ctx.today);
        const end = today.addDays(7);
        for (state.projects.items) |project| {
            if (project.status != .active) continue;
            const summary = try state.projectSummary(project.id, ctx.today);
            const include = if (std.mem.eql(u8, name, "projects_without_next_actions"))
                summary.open_tasks > 0 and summary.next_action_tasks == 0
            else if (std.mem.eql(u8, name, "stalled_projects"))
                summary.open_tasks > 0 and (summary.blocked_tasks > 0 or summary.dependency_blocked_tasks > 0)
            else if (std.mem.eql(u8, name, "projects_missing_deadlines"))
                project.deadline == null
            else blk: {
                const deadline = if (project.deadline) |value| dates.Date.parse(value) catch null else null;
                if (deadline == null) break :blk false;
                const in_window = deadline.?.compare(today) != .lt and deadline.?.compare(end) != .gt;
                if (std.mem.eql(u8, name, "deadline_projects")) break :blk in_window;
                break :blk in_window and (summary.overdue_tasks > 0 or summary.blocked_tasks > 0 or summary.dependency_blocked_tasks > 0 or summary.next_action_tasks == 0);
            };
            if (!include) continue;
            if (!first) try out.appendSlice(ctx.allocator, ", ");
            first = false;
            try out.appendSlice(ctx.allocator, try json.projectView(ctx.allocator, project, summary));
        }
        try out.appendSlice(ctx.allocator, "]");
    }
}

fn renderSectionsPlain(ctx: Context, title: []const u8, state: *const domain.AppState, sections: []const TaskSection) ![]const u8 {
    var out: std.ArrayList(u8) = .empty;
    try out.appendSlice(ctx.allocator, try render.accent(ctx.allocator, ctx.runtime, try std.fmt.allocPrint(ctx.allocator, "== {s} ==", .{title})));
    var any = false;
    for (sections) |section| {
        if (section.tasks.len == 0) continue;
        any = true;
        try out.print(ctx.allocator, "\n-- {s} --", .{section.name});
        for (section.tasks) |task| {
            const project = if (task.project_id) |id| state.projectName(id) orelse "inbox" else "inbox";
            try out.print(ctx.allocator, "\n  [{d}] {s:<11} {s:<11} {s}", .{ task.id, task.due_date orelse "none", project, task.title });
        }
    }
    if (!any) try out.appendSlice(ctx.allocator, "\nNothing to review.");
    return out.toOwnedSlice(ctx.allocator);
}

fn applyReviewActions(ctx: Context, state: *domain.AppState, args: []const []const u8) !std.ArrayList([]const u8) {
    var actions: std.ArrayList([]const u8) = .empty;
    var i: usize = 0;
    while (i < args.len) : (i += 1) {
        const arg = args[i];
        if (std.mem.eql(u8, arg, "--next-action") or std.mem.eql(u8, arg, "--start") or std.mem.eql(u8, arg, "--waiting") or std.mem.eql(u8, arg, "--blocked") or std.mem.eql(u8, arg, "--complete") or std.mem.eql(u8, arg, "--archive")) {
            i += 1;
            if (i >= args.len) break;
            const id = parseId(args[i]) orelse continue;
            const status: domain.TaskStatus = if (std.mem.eql(u8, arg, "--next-action")) .next_action else if (std.mem.eql(u8, arg, "--start")) .in_progress else if (std.mem.eql(u8, arg, "--waiting")) .waiting else if (std.mem.eql(u8, arg, "--blocked")) .blocked else if (std.mem.eql(u8, arg, "--archive")) .archived else .done;
            _ = try state.setTaskStatus(id, status, ctx.today);
            try actions.append(ctx.allocator, try std.fmt.allocPrint(ctx.allocator, "{s} task {d}", .{ if (status == .done) "completed" else "updated", id }));
        } else if (std.mem.eql(u8, arg, "--defer")) {
            i += 1;
            if (i >= args.len) break;
            const parsed = parseIdDate(args[i]) orelse continue;
            try state.applyTaskPatch(parsed.id, .{ .due_date = .{ .set = try resolveDateAlloc(ctx, parsed.date) } }, ctx.today);
            try actions.append(ctx.allocator, try std.fmt.allocPrint(ctx.allocator, "deferred task {d}", .{parsed.id}));
        } else if (std.mem.eql(u8, arg, "--plan")) {
            i += 1;
            if (i >= args.len) break;
            const split = std.mem.indexOfScalar(u8, args[i], ':') orelse continue;
            const project_ref = args[i][0..split];
            const title = args[i][split + 1 ..];
            const project_id = try state.resolveProjectId(project_ref);
            const task = try state.createTask(.{ .title = title, .project_id = project_id, .tags = &.{"next-action"} }, ctx.today);
            _ = try state.setTaskStatus(task.id, .next_action, ctx.today);
            try actions.append(ctx.allocator, try std.fmt.allocPrint(ctx.allocator, "planned next action {d}", .{task.id}));
        }
    }
    return actions;
}

const IdDate = struct { id: u64, date: []const u8 };

fn parseIdDate(value: []const u8) ?IdDate {
    const split = std.mem.indexOfScalar(u8, value, ':') orelse return null;
    return .{ .id = parseId(value[0..split]) orelse return null, .date = value[split + 1 ..] };
}

fn sortTasks(tasks: []const *const domain.Task, sort_key: config.TaskSortKey) void {
    const mutable: []*const domain.Task = @constCast(tasks);
    std.mem.sort(*const domain.Task, mutable, sort_key, taskLess);
}

fn sortReadyTasks(tasks: []const *const domain.Task) void {
    const mutable: []*const domain.Task = @constCast(tasks);
    std.mem.sort(*const domain.Task, mutable, {}, readyLess);
}

fn taskLess(sort_key: config.TaskSortKey, left: *const domain.Task, right: *const domain.Task) bool {
    const primary = switch (sort_key) {
        .due => compareDue(left.*, right.*),
        .priority => std.math.order(right.priority.rank(), left.priority.rank()),
        .updated => compareStringsDesc(left.updated_on, right.updated_on),
        .title => compareStringsAsc(left.title, right.title),
    };
    if (primary != .eq) return primary == .lt;
    const fallback = compareDue(left.*, right.*);
    if (fallback != .eq) return fallback == .lt;
    if (left.priority.rank() != right.priority.rank()) return left.priority.rank() > right.priority.rank();
    return left.id < right.id;
}

fn readyLess(_: void, left: *const domain.Task, right: *const domain.Task) bool {
    if (left.status.isNextAction() != right.status.isNextAction()) return left.status.isNextAction();
    return taskLess(.due, left, right);
}

fn compareDue(left: domain.Task, right: domain.Task) std.math.Order {
    if (left.due_date != null and right.due_date != null) {
        const l = dates.Date.parse(left.due_date.?) catch return .eq;
        const r = dates.Date.parse(right.due_date.?) catch return .eq;
        return l.compare(r);
    }
    if (left.due_date != null and right.due_date == null) return .lt;
    if (left.due_date == null and right.due_date != null) return .gt;
    return .eq;
}

fn compareStringsAsc(left: []const u8, right: []const u8) std.math.Order {
    if (std.ascii.lessThanIgnoreCase(left, right)) return .lt;
    if (std.ascii.lessThanIgnoreCase(right, left)) return .gt;
    return .eq;
}

fn compareStringsDesc(left: []const u8, right: []const u8) std.math.Order {
    return compareStringsAsc(right, left);
}

fn lessProjectEntry(_: void, left: render.ProjectEntry, right: render.ProjectEntry) bool {
    return std.ascii.lessThanIgnoreCase(left.project.name, right.project.name);
}

fn isReadyTask(state: *const domain.AppState, task: domain.Task) bool {
    return task.project_id != null and (task.status == .todo or task.status == .next_action or task.status == .in_progress) and state.taskInActiveProject(task) and !state.hasUnresolvedDependencies(task);
}

fn dateEquals(value: ?[]const u8, date: dates.Date) bool {
    const parsed = if (value) |v| dates.Date.parse(v) catch return false else return false;
    return parsed.compare(date) == .eq;
}

fn dateBefore(value: ?[]const u8, date: dates.Date) bool {
    const parsed = if (value) |v| dates.Date.parse(v) catch return false else return false;
    return parsed.compare(date) == .lt;
}

fn resolveDateAlloc(ctx: Context, value: []const u8) ![]const u8 {
    const today = try dates.Date.parse(ctx.today);
    const resolved = dates.resolveExpression(ctx.allocator, today, value) catch return error.InvalidDateExpression;
    return resolved.formatAlloc(ctx.allocator);
}

fn readOptionalText(allocator: std.mem.Allocator, inline_text: ?[]const u8, file: ?[]const u8) !?[]const u8 {
    if (file) |path| {
        const text: []const u8 = try readFileText(allocator, path);
        return text;
    }
    if (inline_text) |value| {
        const cleaned = std.mem.trim(u8, value, " \t\r\n");
        return if (cleaned.len == 0) null else try allocator.dupe(u8, cleaned);
    }
    return null;
}

fn readFileText(allocator: std.mem.Allocator, path: []const u8) ![]const u8 {
    const contents = try std.fs.cwd().readFileAlloc(allocator, path, 16 * 1024 * 1024);
    return std.mem.trimRight(u8, contents, "\r\n");
}

fn collectStringValues(allocator: std.mem.Allocator, args: []const []const u8, name: []const u8) ![]const []const u8 {
    var out: std.ArrayList([]const u8) = .empty;
    for (args, 0..) |arg, i| {
        if (std.mem.eql(u8, arg, name) and i + 1 < args.len) try out.append(allocator, args[i + 1]);
        if (std.mem.startsWith(u8, arg, name) and arg.len > name.len and arg[name.len] == '=') try out.append(allocator, arg[name.len + 1 ..]);
    }
    return out.toOwnedSlice(allocator);
}

fn collectU64Values(allocator: std.mem.Allocator, args: []const []const u8, name: []const u8) ![]const u64 {
    var out: std.ArrayList(u64) = .empty;
    for (args, 0..) |arg, i| {
        if (std.mem.eql(u8, arg, name) and i + 1 < args.len) {
            if (parseId(args[i + 1])) |id| try out.append(allocator, id);
        }
        if (std.mem.startsWith(u8, arg, name) and arg.len > name.len and arg[name.len] == '=') {
            if (parseId(arg[name.len + 1 ..])) |id| try out.append(allocator, id);
        }
    }
    return out.toOwnedSlice(allocator);
}

fn hasFlag(args: []const []const u8, name: []const u8) bool {
    for (args) |arg| if (std.mem.eql(u8, arg, name)) return true;
    return false;
}

fn parseId(value: []const u8) ?u64 {
    return std.fmt.parseInt(u64, value, 10) catch null;
}

fn containsIgnoreCase(haystack: []const u8, needle: []const u8) bool {
    if (needle.len == 0) return true;
    if (needle.len > haystack.len) return false;
    var i: usize = 0;
    while (i + needle.len <= haystack.len) : (i += 1) {
        if (std.ascii.eqlIgnoreCase(haystack[i .. i + needle.len], needle)) return true;
    }
    return false;
}

fn parseGlobalOptions(allocator: std.mem.Allocator, args: []const []const u8) !GlobalOptions {
    var out = GlobalOptions{};
    var command_args: std.ArrayList([]const u8) = .empty;
    var i: usize = 1;
    while (i < args.len) : (i += 1) {
        const arg = args[i];
        if (std.mem.eql(u8, arg, "--help") or std.mem.eql(u8, arg, "-h")) {
            if (command_args.items.len == 0) out.help = true else try command_args.append(allocator, arg);
        } else if (std.mem.eql(u8, arg, "--version") or std.mem.eql(u8, arg, "-V")) {
            out.version = true;
        } else if (std.mem.eql(u8, arg, "--json")) {
            out.requested_output = .json;
        } else if (std.mem.startsWith(u8, arg, "--output=")) {
            out.requested_output = parseOutput(arg["--output=".len..]) orelse .plain;
        } else if (std.mem.eql(u8, arg, "--output")) {
            i += 1;
            if (i < args.len) out.requested_output = parseOutput(args[i]) orelse .plain;
        } else if (std.mem.startsWith(u8, arg, "--color=")) {
            out.color = parseColor(arg["--color=".len..]) orelse .auto;
        } else if (std.mem.eql(u8, arg, "--color")) {
            i += 1;
            if (i < args.len) out.color = parseColor(args[i]) orelse .auto;
        } else if (std.mem.startsWith(u8, arg, "--data-dir=")) {
            out.data_dir = arg["--data-dir=".len..];
        } else if (std.mem.eql(u8, arg, "--data-dir")) {
            i += 1;
            if (i < args.len) out.data_dir = args[i];
        } else {
            try command_args.append(allocator, arg);
        }
    }
    out.command_args = try command_args.toOwnedSlice(allocator);
    return out;
}

fn parseOutput(value: []const u8) ?OutputFormat {
    if (std.mem.eql(u8, value, "json")) return .json;
    if (std.mem.eql(u8, value, "plain")) return .plain;
    return null;
}

fn parseColor(value: []const u8) ?render.ColorMode {
    if (std.mem.eql(u8, value, "always")) return .always;
    if (std.mem.eql(u8, value, "never")) return .never;
    if (std.mem.eql(u8, value, "auto")) return .auto;
    return null;
}

fn hasHelp(args: []const []const u8) bool {
    for (args) |arg| if (std.mem.eql(u8, arg, "--help") or std.mem.eql(u8, arg, "-h")) return true;
    return false;
}

fn commandHelp(allocator: std.mem.Allocator, args: []const []const u8) ![]const u8 {
    if (args.len >= 3 and std.mem.eql(u8, args[0], "task") and std.mem.eql(u8, args[1], "add")) {
        return allocator.dupe(u8,
            \\Create a task.
            \\
            \\Options:
            \\  --title <TITLE>
            \\  --notes-file <NOTES_FILE>
            \\  --output <OUTPUT>
            \\  --color <COLOR>
            \\  --data-dir <DATA_DIR>
            \\
        );
    }
    if (args.len >= 3 and std.mem.eql(u8, args[0], "review")) {
        return allocator.dupe(u8,
            \\Run a review.
            \\
            \\Options:
            \\  --defer <ID:DATE>
            \\
            \\Defer a task with ID:DATE. DATE accepts YYYY-MM-DD, today, tomorrow, next-week, next-month, next-monday, or +3d.
            \\
        );
    }
    return topLevelHelp(allocator);
}

fn topLevelHelp(allocator: std.mem.Allocator) ![]const u8 {
    return allocator.dupe(u8,
        \\Kelp is a local-first planner for explicit task and project workflows.
        \\
        \\Usage:
        \\  kelp
        \\  kelp init
        \\  kelp --output json task add --title "Draft release notes" --project Launch --due next-monday
        \\  kelp task ready --limit 10
        \\  kelp review weekly
        \\  kelp storage export --file ./kelp-export.json
        \\
        \\Global options:
        \\  --output <OUTPUT>
        \\  --json
        \\  --color <COLOR>
        \\  --data-dir <DATA_DIR>
        \\
    );
}

fn executeCompletions(ctx: Context, args: []const []const u8) !CommandResult {
    if (args.len == 0) return .{ .err = errors.usage("missing_shell", "shell is required") };
    _ = ctx;
    if (std.mem.eql(u8, args[0], "bash")) return .{ .ok = BASH_COMPLETION };
    if (std.mem.eql(u8, args[0], "zsh")) return .{ .ok = ZSH_COMPLETION };
    if (std.mem.eql(u8, args[0], "fish")) return .{ .ok = FISH_COMPLETION };
    return .{ .err = errors.usage("unsupported_shell", "unsupported shell") };
}

const BASH_COMPLETION =
    \\_kelp() { COMPREPLY=( $(compgen -W "init config import storage task project today upcoming review search completions add list ready show edit bulk-edit next start wait block done reopen defer archive unarchive delete" -- "${COMP_WORDS[COMP_CWORD]}") ); }
    \\complete -F _kelp kelp
    \\
;
const ZSH_COMPLETION = "#compdef kelp\n_arguments '*: :((init config import storage task project today upcoming review search completions next blocked edit))'\n";
const FISH_COMPLETION = "complete -c kelp -f -a 'init config import storage task project today upcoming review search completions next blocked edit'\n";

fn ensureTrailingNewline(allocator: std.mem.Allocator, value: []const u8) ![]const u8 {
    if (value.len == 0 or value[value.len - 1] == '\n') return value;
    return std.fmt.allocPrint(allocator, "{s}\n", .{value});
}

fn envExists(name: []const u8) bool {
    const value = std.process.getEnvVarOwned(std.heap.page_allocator, name) catch return false;
    std.heap.page_allocator.free(value);
    return true;
}

fn flagValue(args: []const []const u8, name: []const u8) ?[]const u8 {
    for (args, 0..) |arg, i| {
        if (std.mem.eql(u8, arg, name) and i + 1 < args.len) return args[i + 1];
        if (std.mem.startsWith(u8, arg, name) and arg.len > name.len and arg[name.len] == '=') return arg[name.len + 1 ..];
    }
    return null;
}

fn joinLines(allocator: std.mem.Allocator, lines: []const []const u8) ![]const u8 {
    var out: std.ArrayList(u8) = .empty;
    for (lines, 0..) |line_text, index| {
        if (index > 0) try out.append(allocator, '\n');
        try out.appendSlice(allocator, line_text);
    }
    return out.toOwnedSlice(allocator);
}
