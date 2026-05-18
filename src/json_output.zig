const std = @import("std");
const domain = @import("domain.zig");
const errors = @import("errors.zig");

pub const CURRENT_OUTPUT_SCHEMA_VERSION: u32 = 1;

pub fn appendEscaped(out: *std.ArrayList(u8), allocator: std.mem.Allocator, value: []const u8) !void {
    try out.append(allocator, '"');
    for (value) |c| {
        switch (c) {
            '"' => try out.appendSlice(allocator, "\\\""),
            '\\' => try out.appendSlice(allocator, "\\\\"),
            '\n' => try out.appendSlice(allocator, "\\n"),
            '\r' => try out.appendSlice(allocator, "\\r"),
            '\t' => try out.appendSlice(allocator, "\\t"),
            else => {
                if (c < 0x20) {
                    try out.print(allocator, "\\u{x:0>4}", .{c});
                } else {
                    try out.append(allocator, c);
                }
            },
        }
    }
    try out.append(allocator, '"');
}

pub fn appendOptionalString(out: *std.ArrayList(u8), allocator: std.mem.Allocator, value: ?[]const u8) !void {
    if (value) |text| try appendEscaped(out, allocator, text) else try out.appendSlice(allocator, "null");
}

pub fn successEnvelope(allocator: std.mem.Allocator, command: []const u8, data: []const u8) ![]const u8 {
    var out: std.ArrayList(u8) = .empty;
    try out.print(allocator,
        \\{{
        \\  "schema_version": {d},
        \\  "command": 
    , .{CURRENT_OUTPUT_SCHEMA_VERSION});
    try appendEscaped(&out, allocator, command);
    try out.appendSlice(allocator,
        \\,
        \\  "data": 
    );
    try out.appendSlice(allocator, data);
    try out.appendSlice(allocator, "\n}\n");
    return out.toOwnedSlice(allocator);
}

pub fn errorEnvelope(allocator: std.mem.Allocator, report: errors.ErrorReport) ![]const u8 {
    var out: std.ArrayList(u8) = .empty;
    try out.print(allocator,
        \\{{
        \\  "schema_version": {d},
        \\  "error": {{
        \\    "code": 
    , .{CURRENT_OUTPUT_SCHEMA_VERSION});
    try appendEscaped(&out, allocator, report.code);
    try out.appendSlice(allocator, ",\n    \"message\": ");
    try appendEscaped(&out, allocator, report.message);
    try out.appendSlice(allocator, ",\n    \"details\": [");
    for (report.details, 0..) |detail, index| {
        if (index > 0) try out.appendSlice(allocator, ", ");
        try appendEscaped(&out, allocator, detail);
    }
    try out.appendSlice(allocator, "]\n  }\n}\n");
    return out.toOwnedSlice(allocator);
}

pub fn errorPlain(allocator: std.mem.Allocator, report: errors.ErrorReport) ![]const u8 {
    var out: std.ArrayList(u8) = .empty;
    try out.print(allocator, "error: {s}", .{report.message});
    if (report.details.len > 0) {
        try out.appendSlice(allocator, "\ncaused by:");
        for (report.details) |detail| try out.print(allocator, "\n- {s}", .{detail});
    }
    try out.append(allocator, '\n');
    return out.toOwnedSlice(allocator);
}

pub fn taskView(allocator: std.mem.Allocator, state: *const domain.AppState, task: domain.Task) ![]const u8 {
    var out: std.ArrayList(u8) = .empty;
    try out.print(allocator, "{{\n  \"id\": {d},\n  \"title\": ", .{task.id});
    try appendEscaped(&out, allocator, task.title);
    try out.appendSlice(allocator, ",\n  \"notes\": ");
    try appendOptionalString(&out, allocator, task.notes);
    try out.appendSlice(allocator, ",\n  \"project\": ");
    const project = if (task.project_id) |id| state.projectName(id) else null;
    try appendOptionalString(&out, allocator, project);
    try out.print(allocator, ",\n  \"status\": \"{s}\",\n  \"priority\": \"{s}\",\n  \"tags\": [", .{ task.status.label(), task.priority.label() });
    for (task.tags, 0..) |tag, index| {
        if (index > 0) try out.appendSlice(allocator, ", ");
        try appendEscaped(&out, allocator, tag);
    }
    try out.appendSlice(allocator, "],\n  \"due_date\": ");
    try appendOptionalString(&out, allocator, task.due_date);
    try out.appendSlice(allocator, ",\n  \"recurrence\": ");
    if (task.recurrence) |rule| {
        try appendEscaped(&out, allocator, rule.label());
    } else try out.appendSlice(allocator, "null");
    try out.appendSlice(allocator, ",\n  \"created_on\": ");
    try appendEscaped(&out, allocator, task.created_on);
    try out.appendSlice(allocator, ",\n  \"updated_on\": ");
    try appendEscaped(&out, allocator, task.updated_on);
    try out.appendSlice(allocator, ",\n  \"completed_on\": ");
    try appendOptionalString(&out, allocator, task.completed_on);
    try out.appendSlice(allocator, ",\n  \"archived_on\": ");
    try appendOptionalString(&out, allocator, task.archived_on);
    try out.appendSlice(allocator, ",\n  \"waiting_until\": ");
    try appendOptionalString(&out, allocator, task.waiting_until);
    try out.appendSlice(allocator, ",\n  \"blocked_reason\": ");
    try appendOptionalString(&out, allocator, task.blocked_reason);
    try out.appendSlice(allocator, ",\n  \"depends_on\": [");
    for (task.depends_on, 0..) |dep, index| {
        if (index > 0) try out.appendSlice(allocator, ", ");
        try out.print(allocator, "{d}", .{dep});
    }
    const unresolved = try state.unresolvedTaskDependencies(task, allocator);
    try out.appendSlice(allocator, "],\n  \"unresolved_dependencies\": [");
    for (unresolved, 0..) |dep, index| {
        if (index > 0) try out.appendSlice(allocator, ", ");
        try out.print(allocator, "{d}", .{dep});
    }
    try out.appendSlice(allocator, "]\n}");
    return out.toOwnedSlice(allocator);
}

pub fn projectView(allocator: std.mem.Allocator, project: domain.Project, summary: domain.ProjectSummary) ![]const u8 {
    var out: std.ArrayList(u8) = .empty;
    try out.print(allocator, "{{\n  \"id\": {d},\n  \"name\": ", .{project.id});
    try appendEscaped(&out, allocator, project.name);
    try out.appendSlice(allocator, ",\n  \"description\": ");
    try appendOptionalString(&out, allocator, project.description);
    try out.print(allocator, ",\n  \"status\": \"{s}\",\n  \"created_on\": ", .{project.status.label()});
    try appendEscaped(&out, allocator, project.created_on);
    try out.appendSlice(allocator, ",\n  \"updated_on\": ");
    try appendEscaped(&out, allocator, project.updated_on);
    try out.appendSlice(allocator, ",\n  \"archived_on\": ");
    try appendOptionalString(&out, allocator, project.archived_on);
    try out.appendSlice(allocator, ",\n  \"deadline\": ");
    try appendOptionalString(&out, allocator, project.deadline);
    try out.print(allocator,
        \\,
        \\  "summary": {{
        \\    "total_tasks": {d},
        \\    "open_tasks": {d},
        \\    "completed_tasks": {d},
        \\    "overdue_tasks": {d},
        \\    "next_action_tasks": {d},
        \\    "waiting_tasks": {d},
        \\    "blocked_tasks": {d},
        \\    "dependency_blocked_tasks": {d},
        \\    "completion_percent": {d}
        \\  }}
        \\}}
    , .{ summary.total_tasks, summary.open_tasks, summary.completed_tasks, summary.overdue_tasks, summary.next_action_tasks, summary.waiting_tasks, summary.blocked_tasks, summary.dependency_blocked_tasks, summary.completion_percent });
    return out.toOwnedSlice(allocator);
}
