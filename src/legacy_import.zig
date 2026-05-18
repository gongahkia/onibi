const std = @import("std");
const domain = @import("domain.zig");

pub const LegacyImportSummary = struct {
    imported_tasks: usize = 0,
    imported_projects: usize = 0,
    reused_projects: usize = 0,
    skipped_duplicates: usize = 0,
    scanned_files: usize = 0,
    warnings: []const []const u8 = &.{},
};

pub fn importLegacyFromPath(allocator: std.mem.Allocator, state: *domain.AppState, source: []const u8, today: []const u8) !LegacyImportSummary {
    var warnings: std.ArrayList([]const u8) = .empty;
    var summary = LegacyImportSummary{};
    const stat = std.fs.cwd().statFile(source) catch null;
    if (stat != null and stat.?.kind == .file) {
        try importStorageFile(allocator, state, source, null, today, &summary, &warnings);
        summary.warnings = try warnings.toOwnedSlice(allocator);
        return summary;
    }

    const inbox = try std.fs.path.join(allocator, &.{ source, ".kelpStorage" });
    if (exists(inbox)) try importStorageFile(allocator, state, inbox, null, today, &summary, &warnings);

    const projects_dir = try std.fs.path.join(allocator, &.{ source, ".kelpProjects" });
    var dir = std.fs.cwd().openDir(projects_dir, .{ .iterate = true }) catch {
        summary.warnings = try warnings.toOwnedSlice(allocator);
        return summary;
    };
    defer dir.close();
    var entries: std.ArrayList([]const u8) = .empty;
    var it = dir.iterate();
    while (try it.next()) |entry| {
        if (entry.kind == .directory) try entries.append(allocator, try allocator.dupe(u8, entry.name));
    }
    std.mem.sort([]const u8, entries.items, {}, lessString);
    for (entries.items) |project_name| {
        const project_id = try ensureProject(state, project_name, today, &summary);
        const storage_file = try std.fs.path.join(allocator, &.{ projects_dir, project_name, ".kelpStorage" });
        if (exists(storage_file)) try importStorageFile(allocator, state, storage_file, project_id, today, &summary, &warnings);
    }
    summary.warnings = try warnings.toOwnedSlice(allocator);
    return summary;
}

fn ensureProject(state: *domain.AppState, project_name: []const u8, today: []const u8, summary: *LegacyImportSummary) !u64 {
    for (state.projects.items) |project| {
        if (std.ascii.eqlIgnoreCase(project.name, project_name)) {
            if (project.status != .active) try state.activateProject(project.id, today);
            summary.reused_projects += 1;
            return project.id;
        }
    }
    const project = try state.createProject(project_name, "Imported from legacy Kelp", null, today);
    summary.imported_projects += 1;
    return project.id;
}

fn importStorageFile(
    allocator: std.mem.Allocator,
    state: *domain.AppState,
    path: []const u8,
    project_id: ?u64,
    today: []const u8,
    summary: *LegacyImportSummary,
    warnings: *std.ArrayList([]const u8),
) !void {
    const contents = try std.fs.cwd().readFileAlloc(allocator, path, 16 * 1024 * 1024);
    summary.scanned_files += 1;
    var line_it = std.mem.splitScalar(u8, contents, '\n');
    var line_index: usize = 0;
    while (line_it.next()) |raw| {
        line_index += 1;
        const line = std.mem.trim(u8, raw, " \t\r\n");
        if (line.len == 0) continue;
        const row = parseLegacyTaskLine(allocator, line) catch |err| {
            try warnings.append(allocator, try std.fmt.allocPrint(allocator, "{s}:{d} {s}", .{ path, line_index, @errorName(err) }));
            continue;
        };
        if (isDuplicate(state, row, project_id)) {
            summary.skipped_duplicates += 1;
            continue;
        }
        _ = try state.createTask(.{
            .title = row.title,
            .notes = row.notes,
            .project_id = project_id,
            .priority = row.priority,
            .tags = row.tags,
            .due_date = row.due_date,
        }, today);
        summary.imported_tasks += 1;
    }
}

const LegacyTaskRow = struct {
    title: []const u8,
    notes: ?[]const u8,
    due_date: []const u8,
    priority: domain.Priority,
    tags: []const []const u8,
};

fn parseLegacyTaskLine(allocator: std.mem.Allocator, line: []const u8) !LegacyTaskRow {
    var parts: [5][]const u8 = undefined;
    var start: usize = 0;
    var count: usize = 0;
    while (count < 4) : (count += 1) {
        const rest = line[start..];
        const idx = std.mem.indexOf(u8, rest, ", ") orelse return error.InvalidLegacyRow;
        parts[count] = rest[0..idx];
        start += idx + 2;
    }
    parts[4] = line[start..];
    const priority = if (std.mem.eql(u8, std.mem.trim(u8, parts[3], " \t\r\n"), "Low"))
        domain.Priority.low
    else if (std.mem.eql(u8, std.mem.trim(u8, parts[3], " \t\r\n"), "Medium"))
        domain.Priority.medium
    else if (std.mem.eql(u8, std.mem.trim(u8, parts[3], " \t\r\n"), "High"))
        domain.Priority.high
    else
        return error.InvalidLegacyPriority;

    var tags: std.ArrayList([]const u8) = .empty;
    var tag_it = std.mem.splitScalar(u8, parts[4], '&');
    while (tag_it.next()) |tag| {
        const cleaned = std.mem.trim(u8, tag, " \t\r\n");
        if (cleaned.len > 0) try tags.append(allocator, try lowerAlloc(allocator, cleaned));
    }
    return .{
        .title = try allocator.dupe(u8, std.mem.trim(u8, parts[0], " \t\r\n")),
        .notes = blk: {
            const notes = std.mem.trim(u8, parts[1], " \t\r\n");
            break :blk if (notes.len == 0) null else try allocator.dupe(u8, notes);
        },
        .due_date = try parseLegacyDate(allocator, parts[2]),
        .priority = priority,
        .tags = try tags.toOwnedSlice(allocator),
    };
}

fn parseLegacyDate(allocator: std.mem.Allocator, raw: []const u8) ![]const u8 {
    const trimmed = std.mem.trimRight(u8, std.mem.trim(u8, raw, " \t\r\n"), "/");
    var it = std.mem.splitScalar(u8, trimmed, '/');
    const day = try std.fmt.parseInt(u8, std.mem.trim(u8, it.next() orelse return error.InvalidLegacyDate, " "), 10);
    const month = try std.fmt.parseInt(u8, std.mem.trim(u8, it.next() orelse return error.InvalidLegacyDate, " "), 10);
    var year = try std.fmt.parseInt(i32, std.mem.trim(u8, it.next() orelse return error.InvalidLegacyDate, " "), 10);
    if (it.next() != null) return error.InvalidLegacyDate;
    if (year < 100) year += 2000;
    return std.fmt.allocPrint(allocator, "{d:0>4}-{d:0>2}-{d:0>2}", .{ @as(u32, @intCast(year)), month, day });
}

fn isDuplicate(state: *const domain.AppState, row: LegacyTaskRow, project_id: ?u64) bool {
    for (state.tasks.items) |task| {
        if (!std.ascii.eqlIgnoreCase(task.title, row.title)) continue;
        if (!std.mem.eql(u8, task.notes orelse "", row.notes orelse "")) continue;
        if (task.project_id != project_id) continue;
        if (task.priority != row.priority) continue;
        if (!std.mem.eql(u8, task.due_date orelse "none", row.due_date)) continue;
        if (!sameTags(task.tags, row.tags)) continue;
        return true;
    }
    return false;
}

fn sameTags(a: []const []const u8, b: []const []const u8) bool {
    if (a.len != b.len) return false;
    for (a) |tag| {
        var found = false;
        for (b) |other| {
            if (std.mem.eql(u8, tag, other)) {
                found = true;
                break;
            }
        }
        if (!found) return false;
    }
    return true;
}

fn lowerAlloc(allocator: std.mem.Allocator, value: []const u8) ![]const u8 {
    const out = try allocator.alloc(u8, value.len);
    for (value, 0..) |c, i| out[i] = std.ascii.toLower(c);
    return out;
}

fn exists(path: []const u8) bool {
    std.fs.cwd().access(path, .{}) catch return false;
    return true;
}

fn lessString(_: void, a: []const u8, b: []const u8) bool {
    return std.mem.lessThan(u8, a, b);
}
