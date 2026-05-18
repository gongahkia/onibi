const std = @import("std");
const domain = @import("domain.zig");

pub const ColorMode = enum { auto, always, never };

pub const RenderOptions = struct {
    color: ColorMode = .auto,

    pub fn shouldColorize(self: RenderOptions) bool {
        return switch (self.color) {
            .always => true,
            .never => false,
            .auto => std.fs.File.stdout().isTty() and std.process.getEnvVarOwned(std.heap.page_allocator, "NO_COLOR") == error.EnvironmentVariableNotFound,
        };
    }
};

pub fn confirmation(allocator: std.mem.Allocator, options: RenderOptions, title: []const u8, body: []const u8) ![]const u8 {
    const h = try heading(allocator, options, title);
    return std.fmt.allocPrint(allocator, "{s}\n{s}", .{ h, body });
}

pub fn init(allocator: std.mem.Allocator, options: RenderOptions, path: []const u8) ![]const u8 {
    return confirmation(allocator, options, "Kelp initialized", try std.fmt.allocPrint(allocator, "{s} {s}", .{ try muted(allocator, options, "data file:"), path }));
}

pub fn taskDetail(allocator: std.mem.Allocator, options: RenderOptions, task: domain.Task, state: *const domain.AppState) ![]const u8 {
    var out: std.ArrayList(u8) = .empty;
    try out.print(allocator, "{s}\n", .{try heading(allocator, options, try std.fmt.allocPrint(allocator, "Task {d}", .{task.id}))});
    try line(&out, allocator, options, "title:", task.title);
    try line(&out, allocator, options, "status:", task.status.label());
    try line(&out, allocator, options, "priority:", task.priority.label());
    try line(&out, allocator, options, "project:", if (task.project_id) |id| state.projectName(id) orelse "none" else "none");
    try line(&out, allocator, options, "due:", task.due_date orelse "none");
    try line(&out, allocator, options, "waiting until:", task.waiting_until orelse "none");
    try line(&out, allocator, options, "blocked reason:", task.blocked_reason orelse "none");
    try line(&out, allocator, options, "depends on:", try formatU64List(allocator, task.depends_on));
    try line(&out, allocator, options, "repeat:", if (task.recurrence) |rule| rule.label() else "none");
    try line(&out, allocator, options, "tags:", try formatTags(allocator, task.tags));
    try line(&out, allocator, options, "created:", task.created_on);
    try line(&out, allocator, options, "updated:", task.updated_on);
    try line(&out, allocator, options, "completed:", task.completed_on orelse "not completed");
    if (task.notes) |notes| try line(&out, allocator, options, "notes:", notes);
    return out.toOwnedSlice(allocator);
}

pub fn taskList(allocator: std.mem.Allocator, options: RenderOptions, title: []const u8, tasks: []const *const domain.Task, state: *const domain.AppState) ![]const u8 {
    var out: std.ArrayList(u8) = .empty;
    try out.print(allocator, "{s}\n", .{try heading(allocator, options, title)});
    if (tasks.len == 0) {
        try out.appendSlice(allocator, try muted(allocator, options, "No matching tasks."));
        return out.toOwnedSlice(allocator);
    }
    try out.appendSlice(allocator, try muted(allocator, options, "ID   STATUS       PRI     DUE         PROJECT       TITLE"));
    for (tasks) |task| {
        const project = if (task.project_id) |id| state.projectName(id) orelse "inbox" else "inbox";
        try out.print(allocator, "\n{d:<4} {s:<12} {s:<7} {s:<11} {s:<13} {s}{s}", .{
            task.id,
            task.status.label(),
            task.priority.label(),
            task.due_date orelse "none",
            try truncate(allocator, project, 12),
            task.title,
            try inlineTags(allocator, task.tags),
        });
    }
    return out.toOwnedSlice(allocator);
}

pub fn projectList(allocator: std.mem.Allocator, options: RenderOptions, title: []const u8, entries: []const ProjectEntry) ![]const u8 {
    var out: std.ArrayList(u8) = .empty;
    try out.print(allocator, "{s}\n", .{try heading(allocator, options, title)});
    if (entries.len == 0) {
        try out.appendSlice(allocator, try muted(allocator, options, "No matching projects."));
        return out.toOwnedSlice(allocator);
    }
    try out.appendSlice(allocator, try muted(allocator, options, "ID   STATUS      DONE   OPEN   OVERDUE   DEADLINE     NAME"));
    for (entries) |entry| {
        try out.print(allocator, "\n{d:<4} {s:<11} {d:>3}%   {d:<6} {d:<8} {s:<12} {s}", .{
            entry.project.id,
            entry.project.status.label(),
            entry.summary.completion_percent,
            entry.summary.open_tasks,
            entry.summary.overdue_tasks,
            entry.project.deadline orelse "none",
            entry.project.name,
        });
    }
    return out.toOwnedSlice(allocator);
}

pub const ProjectEntry = struct {
    project: domain.Project,
    summary: domain.ProjectSummary,
};

fn line(out: *std.ArrayList(u8), allocator: std.mem.Allocator, options: RenderOptions, key: []const u8, value: []const u8) !void {
    try out.print(allocator, "{s} {s}\n", .{ try muted(allocator, options, key), value });
}

fn heading(allocator: std.mem.Allocator, options: RenderOptions, title: []const u8) ![]const u8 {
    return accent(allocator, options, try std.fmt.allocPrint(allocator, "== {s} ==", .{title}));
}

pub fn accent(allocator: std.mem.Allocator, options: RenderOptions, value: []const u8) ![]const u8 {
    return paint(allocator, options, value, "\x1b[36m");
}

pub fn muted(allocator: std.mem.Allocator, options: RenderOptions, value: []const u8) ![]const u8 {
    return paint(allocator, options, value, "\x1b[90m");
}

fn paint(allocator: std.mem.Allocator, options: RenderOptions, value: []const u8, code: []const u8) ![]const u8 {
    if (options.shouldColorize()) return std.fmt.allocPrint(allocator, "{s}{s}\x1b[0m", .{ code, value });
    return allocator.dupe(u8, value);
}

fn inlineTags(allocator: std.mem.Allocator, tags: []const []const u8) ![]const u8 {
    if (tags.len == 0) return "";
    var out: std.ArrayList(u8) = .empty;
    for (tags) |tag| try out.print(allocator, "  #{s}", .{tag});
    return out.toOwnedSlice(allocator);
}

fn formatTags(allocator: std.mem.Allocator, tags: []const []const u8) ![]const u8 {
    if (tags.len == 0) return "none";
    var out: std.ArrayList(u8) = .empty;
    for (tags, 0..) |tag, index| {
        if (index > 0) try out.append(allocator, ' ');
        try out.print(allocator, "#{s}", .{tag});
    }
    return out.toOwnedSlice(allocator);
}

fn formatU64List(allocator: std.mem.Allocator, values: []const u64) ![]const u8 {
    if (values.len == 0) return "none";
    var out: std.ArrayList(u8) = .empty;
    for (values, 0..) |value, index| {
        if (index > 0) try out.appendSlice(allocator, ", ");
        try out.print(allocator, "{d}", .{value});
    }
    return out.toOwnedSlice(allocator);
}

fn truncate(allocator: std.mem.Allocator, value: []const u8, width: usize) ![]const u8 {
    if (value.len <= width) return allocator.dupe(u8, value);
    if (width <= 3) return allocator.dupe(u8, value[0..width]);
    return std.fmt.allocPrint(allocator, "{s}...", .{value[0 .. width - 3]});
}
