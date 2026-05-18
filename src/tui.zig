const std = @import("std");
const builtin = @import("builtin");
const domain = @import("domain.zig");
const storage_mod = @import("storage.zig");
const config = @import("config.zig");

const Focus = enum { status, today, projects, tasks, review };

const UiState = struct {
    focus: Focus = .today,
    selected_task: usize = 0,
    selected_project: usize = 0,
    log: []const u8 = "You can hide/focus this panel by pressing '@'",
};

pub fn run(allocator: std.mem.Allocator, store: storage_mod.JsonFileStorage, config_store: config.Store, today: []const u8) !void {
    _ = config_store;
    const stdout = std.fs.File.stdout();
    if (!stdout.isTty() or !std.fs.File.stdin().isTty()) {
        var state = try store.load();
        const screen = try renderScreen(allocator, &state, .{}, today, 100, 32);
        try stdout.writeAll(screen);
        return;
    }

    var raw = try RawMode.enter();
    defer raw.exit() catch {};
    try stdout.writeAll("\x1b[?1049h\x1b[?25l\x1b[2J");
    defer stdout.writeAll("\x1b[?25h\x1b[?1049l") catch {};

    var ui = UiState{};
    while (true) {
        var state = try store.load();
        const size = terminalSize();
        const screen = try renderScreen(allocator, &state, ui, today, size.cols, size.rows);
        try stdout.writeAll("\x1b[H");
        try stdout.writeAll(screen);

        var buf: [8]u8 = undefined;
        const n = try std.fs.File.stdin().read(&buf);
        if (n == 0) continue;
        const key = parseKey(buf[0..n]);
        if (key == 'q') break;
        switch (key) {
            '1' => ui.focus = .status,
            '2' => ui.focus = .today,
            '3' => ui.focus = .projects,
            '4' => ui.focus = .tasks,
            '5' => ui.focus = .review,
            'j' => moveSelection(&ui, &state, 1),
            'k' => moveSelection(&ui, &state, -1),
            'n' => try promptNewTask(allocator, store, today, &ui),
            'p' => try promptNewProject(allocator, store, today, &ui),
            ' ' => try updateSelectedTask(allocator, store, today, ui, .next_action, "marked task as next action"),
            's' => try updateSelectedTask(allocator, store, today, ui, .in_progress, "started task"),
            'w' => try updateSelectedTask(allocator, store, today, ui, .waiting, "marked task as waiting"),
            'b' => try updateSelectedTask(allocator, store, today, ui, .blocked, "marked task as blocked"),
            'd' => try updateSelectedTask(allocator, store, today, ui, .done, "completed task"),
            'a' => try updateSelectedTask(allocator, store, today, ui, .archived, "archived task"),
            'r' => try updateSelectedTask(allocator, store, today, ui, .todo, "reopened task"),
            'x' => try deleteSelectedTask(allocator, store, ui),
            '?' => ui.log = "1-5 focus | j/k move | n task | p project | space next | s start | w wait | b block | d done | a archive | r reopen | x delete | q quit",
            else => {},
        }
    }
}

fn renderScreen(allocator: std.mem.Allocator, state: *domain.AppState, ui: UiState, today: []const u8, cols_raw: u16, rows_raw: u16) ![]const u8 {
    const cols: usize = @max(cols_raw, 80);
    const rows: usize = @max(rows_raw, 24);
    const left_w: usize = @min(42, cols / 3);
    const right_x = left_w + 2;
    const right_w = cols - right_x - 1;
    const command_h: usize = 5;
    const main_h = rows - command_h - 1;
    var out: std.ArrayList(u8) = .empty;
    try out.appendSlice(allocator, "\x1b[40m\x1b[37m\x1b[2J");

    try drawBox(&out, allocator, 1, 1, left_w, 3, "[1]-Status", ui.focus == .status);
    try put(&out, allocator, 2, 2, try std.fmt.allocPrint(allocator, "kelp -> local    {s}", .{today}));

    const panel_h = @max(4, (main_h - 3) / 4);
    try drawListPanel(&out, allocator, state, 1, 4, left_w, panel_h, "[2]-Today - Ready - Upcoming", ui.focus == .today, ui.selected_task, .today);
    try drawProjectPanel(&out, allocator, state, 1, 4 + panel_h, left_w, panel_h, ui.focus == .projects, ui.selected_project);
    try drawListPanel(&out, allocator, state, 1, 4 + panel_h * 2, left_w, panel_h, "[4]-Tasks - Search", ui.focus == .tasks, ui.selected_task, .all);
    try drawListPanel(&out, allocator, state, 1, 4 + panel_h * 3, left_w, main_h - 3 - panel_h * 3, "[5]-Review", ui.focus == .review, ui.selected_task, .review);

    try drawBox(&out, allocator, right_x, 1, right_w, main_h, "[0]-Details", false);
    try drawDetails(&out, allocator, state, ui, right_x + 2, 3, right_w - 3);

    try drawBox(&out, allocator, right_x, main_h + 1, right_w, command_h, "Command log", false);
    try putColor(&out, allocator, right_x + 2, main_h + 2, "\x1b[32m", ui.log);
    try putColor(&out, allocator, 1, rows, "\x1b[36m", "Fetching | Commit: c | Stash: s | Reset: D | Keybindings: ?");
    return out.toOwnedSlice(allocator);
}

const TaskMode = enum { today, all, review };

fn drawListPanel(out: *std.ArrayList(u8), allocator: std.mem.Allocator, state: *domain.AppState, x: usize, y: usize, w: usize, h: usize, title: []const u8, focused: bool, selected: usize, mode: TaskMode) !void {
    try drawBox(out, allocator, x, y, w, h, title, focused);
    var row: usize = 0;
    for (state.tasks.items) |task| {
        if (row >= h - 2) break;
        if (task.status == .archived) continue;
        if (mode == .today and task.due_date == null and !task.status.isNextAction()) continue;
        if (mode == .review and !(task.status == .blocked or task.status == .waiting or task.status.isNextAction())) continue;
        const prefix = if (row == selected and focused) "\x1b[7m" else "";
        const suffix = if (row == selected and focused) "\x1b[0m" else "";
        const line = try std.fmt.allocPrint(allocator, "{s}{d:<3} {s:<11} {s}{s}", .{ prefix, task.id, task.status.label(), task.title, suffix });
        try put(out, allocator, x + 2, y + 1 + row, try ellipsis(allocator, line, w - 3));
        row += 1;
    }
    if (row == 0) try put(out, allocator, x + 2, y + 1, "No tasks");
}

fn drawProjectPanel(out: *std.ArrayList(u8), allocator: std.mem.Allocator, state: *domain.AppState, x: usize, y: usize, w: usize, h: usize, focused: bool, selected: usize) !void {
    try drawBox(out, allocator, x, y, w, h, "[3]-Projects", focused);
    var row: usize = 0;
    for (state.projects.items) |project| {
        if (row >= h - 2) break;
        if (project.status == .archived) continue;
        const prefix = if (row == selected and focused) "\x1b[7m" else "";
        const suffix = if (row == selected and focused) "\x1b[0m" else "";
        try put(out, allocator, x + 2, y + 1 + row, try ellipsis(allocator, try std.fmt.allocPrint(allocator, "{s}{d:<3} {s}{s}", .{ prefix, project.id, project.name, suffix }), w - 3));
        row += 1;
    }
    if (row == 0) try put(out, allocator, x + 2, y + 1, "No projects");
}

fn drawDetails(out: *std.ArrayList(u8), allocator: std.mem.Allocator, state: *domain.AppState, ui: UiState, x: usize, y: usize, w: usize) !void {
    const task = selectedTask(state, ui.selected_task) orelse {
        try put(out, allocator, x, y, "No task selected");
        return;
    };
    var row = y;
    try putColor(out, allocator, x, row, "\x1b[33m", task.title);
    row += 2;
    const fields = [_][]const u8{
        try std.fmt.allocPrint(allocator, "id: {d}", .{task.id}),
        try std.fmt.allocPrint(allocator, "status: {s}", .{task.status.label()}),
        try std.fmt.allocPrint(allocator, "priority: {s}", .{task.priority.label()}),
        try std.fmt.allocPrint(allocator, "project: {s}", .{if (task.project_id) |id| state.projectName(id) orelse "inbox" else "inbox"}),
        try std.fmt.allocPrint(allocator, "due: {s}", .{task.due_date orelse "none"}),
        try std.fmt.allocPrint(allocator, "waiting until: {s}", .{task.waiting_until orelse "none"}),
        try std.fmt.allocPrint(allocator, "blocked reason: {s}", .{task.blocked_reason orelse "none"}),
    };
    for (fields) |field| {
        try put(out, allocator, x, row, try ellipsis(allocator, field, w));
        row += 1;
    }
    if (task.notes) |notes| {
        row += 1;
        try put(out, allocator, x, row, try ellipsis(allocator, notes, w));
    }
}

fn drawBox(out: *std.ArrayList(u8), allocator: std.mem.Allocator, x: usize, y: usize, w: usize, h: usize, title: []const u8, focused: bool) !void {
    const color = if (focused) "\x1b[32m" else "\x1b[90m";
    try putColor(out, allocator, x, y, color, try std.fmt.allocPrint(allocator, "+{s}+", .{try repeat(allocator, "-", w - 2)}));
    try putColor(out, allocator, x + 2, y, if (focused) "\x1b[33m" else "\x1b[37m", title);
    var row: usize = 1;
    while (row + 1 < h) : (row += 1) {
        try putColor(out, allocator, x, y + row, color, "|");
        try putColor(out, allocator, x + w - 1, y + row, color, "|");
    }
    try putColor(out, allocator, x, y + h - 1, color, try std.fmt.allocPrint(allocator, "+{s}+", .{try repeat(allocator, "-", w - 2)}));
}

fn promptNewTask(allocator: std.mem.Allocator, store: storage_mod.JsonFileStorage, today: []const u8, ui: *UiState) !void {
    const title = try promptLine(allocator, "New task title: ");
    if (std.mem.trim(u8, title, " \t\r\n").len == 0) {
        ui.log = "task title cannot be empty";
        return;
    }
    var state = try store.load();
    _ = state.createTask(.{ .title = title }, today) catch |err| {
        ui.log = @errorName(err);
        return;
    };
    try store.save(&state);
    ui.log = "created task";
}

fn promptNewProject(allocator: std.mem.Allocator, store: storage_mod.JsonFileStorage, today: []const u8, ui: *UiState) !void {
    const name = try promptLine(allocator, "New project name: ");
    if (std.mem.trim(u8, name, " \t\r\n").len == 0) {
        ui.log = "project name cannot be empty";
        return;
    }
    var state = try store.load();
    _ = state.createProject(name, null, null, today) catch |err| {
        ui.log = @errorName(err);
        return;
    };
    try store.save(&state);
    ui.log = "created project";
}

fn updateSelectedTask(allocator: std.mem.Allocator, store: storage_mod.JsonFileStorage, today: []const u8, ui: UiState, status: domain.TaskStatus, message: []const u8) !void {
    _ = allocator;
    var state = try store.load();
    const task = selectedTask(&state, ui.selected_task) orelse return;
    _ = try state.setTaskStatus(task.id, status, today);
    try store.save(&state);
    _ = message;
}

fn deleteSelectedTask(allocator: std.mem.Allocator, store: storage_mod.JsonFileStorage, ui: UiState) !void {
    _ = allocator;
    var state = try store.load();
    const task = selectedTask(&state, ui.selected_task) orelse return;
    _ = try state.deleteTask(task.id);
    try store.save(&state);
}

fn promptLine(allocator: std.mem.Allocator, prompt: []const u8) ![]const u8 {
    var raw = try RawMode.pauseRaw();
    defer raw.restore() catch {};
    const stdout = std.fs.File.stdout();
    try stdout.writeAll("\x1b[?25h\x1b[999;1H\x1b[2K");
    try stdout.writeAll(prompt);
    var buf: [4096]u8 = undefined;
    const n = try std.fs.File.stdin().read(&buf);
    try stdout.writeAll("\x1b[?25l");
    return allocator.dupe(u8, std.mem.trimRight(u8, buf[0..n], "\r\n"));
}

fn selectedTask(state: *const domain.AppState, selected: usize) ?*const domain.Task {
    var row: usize = 0;
    for (state.tasks.items) |*task| {
        if (task.status == .archived) continue;
        if (row == selected) return task;
        row += 1;
    }
    return null;
}

fn moveSelection(ui: *UiState, state: *domain.AppState, delta: i32) void {
    const count = state.tasks.items.len;
    if (count == 0) return;
    if (delta > 0 and ui.selected_task + 1 < count) ui.selected_task += 1;
    if (delta < 0 and ui.selected_task > 0) ui.selected_task -= 1;
}

fn parseKey(bytes: []const u8) u8 {
    if (bytes.len >= 3 and bytes[0] == 0x1b and bytes[1] == '[') {
        return switch (bytes[2]) {
            'A' => 'k',
            'B' => 'j',
            'C' => 'l',
            'D' => 'h',
            else => 0,
        };
    }
    return bytes[0];
}

const TermSize = struct { cols: u16, rows: u16 };

fn terminalSize() TermSize {
    const cols = parseEnvU16("COLUMNS") orelse 120;
    const rows = parseEnvU16("LINES") orelse 36;
    return .{ .cols = cols, .rows = rows };
}

fn parseEnvU16(name: []const u8) ?u16 {
    const value = std.process.getEnvVarOwned(std.heap.page_allocator, name) catch return null;
    defer std.heap.page_allocator.free(value);
    return std.fmt.parseInt(u16, value, 10) catch null;
}

const RawMode = struct {
    original: if (builtin.os.tag == .windows) void else std.posix.termios,
    active: bool = false,

    fn enter() !RawMode {
        if (builtin.os.tag == .windows) return .{ .original = {}, .active = false };
        const original = try std.posix.tcgetattr(std.posix.STDIN_FILENO);
        var raw = original;
        raw.lflag.ECHO = false;
        raw.lflag.ICANON = false;
        try std.posix.tcsetattr(std.posix.STDIN_FILENO, .FLUSH, raw);
        return .{ .original = original, .active = true };
    }

    fn exit(self: *RawMode) !void {
        if (builtin.os.tag == .windows or !self.active) return;
        try std.posix.tcsetattr(std.posix.STDIN_FILENO, .FLUSH, self.original);
        self.active = false;
    }

    fn pauseRaw() !RawMode {
        var mode = try RawMode.enter();
        try mode.exit();
        return mode;
    }

    fn restore(self: *RawMode) !void {
        if (builtin.os.tag == .windows) return;
        var raw = self.original;
        raw.lflag.ECHO = false;
        raw.lflag.ICANON = false;
        try std.posix.tcsetattr(std.posix.STDIN_FILENO, .FLUSH, raw);
        self.active = true;
    }
};

fn put(out: *std.ArrayList(u8), allocator: std.mem.Allocator, x: usize, y: usize, text: []const u8) !void {
    try out.print(allocator, "\x1b[{d};{d}H{s}", .{ y, x, text });
}

fn putColor(out: *std.ArrayList(u8), allocator: std.mem.Allocator, x: usize, y: usize, color: []const u8, text: []const u8) !void {
    try out.print(allocator, "\x1b[{d};{d}H{s}{s}\x1b[0m", .{ y, x, color, text });
}

fn repeat(allocator: std.mem.Allocator, text: []const u8, count: usize) ![]const u8 {
    var out: std.ArrayList(u8) = .empty;
    var i: usize = 0;
    while (i < count) : (i += 1) try out.appendSlice(allocator, text);
    return out.toOwnedSlice(allocator);
}

fn ellipsis(allocator: std.mem.Allocator, text: []const u8, width: usize) ![]const u8 {
    if (text.len <= width) return text;
    if (width <= 3) return text[0..width];
    return std.fmt.allocPrint(allocator, "{s}...", .{text[0 .. width - 3]});
}
