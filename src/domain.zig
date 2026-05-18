const std = @import("std");
const dates = @import("dates.zig");

pub const CURRENT_APP_SCHEMA_VERSION: u32 = 5;

pub const Priority = enum {
    low,
    medium,
    high,

    pub fn label(self: Priority) []const u8 {
        return @tagName(self);
    }

    pub fn rank(self: Priority) u8 {
        return switch (self) {
            .high => 3,
            .medium => 2,
            .low => 1,
        };
    }
};

pub const TaskStatus = enum {
    todo,
    next_action,
    in_progress,
    waiting,
    blocked,
    done,
    archived,

    pub fn label(self: TaskStatus) []const u8 {
        return @tagName(self);
    }

    pub fn isOpen(self: TaskStatus) bool {
        return switch (self) {
            .todo, .next_action, .in_progress, .waiting, .blocked => true,
            .done, .archived => false,
        };
    }

    pub fn isNextAction(self: TaskStatus) bool {
        return self == .next_action or self == .in_progress;
    }
};

pub const RecurrenceRule = enum {
    daily,
    weekly,
    monthly,

    pub fn label(self: RecurrenceRule) []const u8 {
        return @tagName(self);
    }

    pub fn nextDueDate(self: RecurrenceRule, current_due_date: []const u8, allocator: std.mem.Allocator) ![]const u8 {
        const due = try dates.Date.parse(current_due_date);
        const next = switch (self) {
            .daily => due.addDays(1),
            .weekly => due.addDays(7),
            .monthly => due.addOneMonth(),
        };
        return next.formatAlloc(allocator);
    }
};

pub const ProjectStatus = enum {
    active,
    archived,

    pub fn label(self: ProjectStatus) []const u8 {
        return @tagName(self);
    }
};

pub const Task = struct {
    id: u64,
    title: []const u8,
    notes: ?[]const u8 = null,
    project_id: ?u64 = null,
    status: TaskStatus = .todo,
    priority: Priority = .medium,
    tags: []const []const u8 = &.{},
    due_date: ?[]const u8 = null,
    recurrence: ?RecurrenceRule = null,
    created_on: []const u8,
    updated_on: []const u8,
    completed_on: ?[]const u8 = null,
    archived_on: ?[]const u8 = null,
    waiting_until: ?[]const u8 = null,
    blocked_reason: ?[]const u8 = null,
    depends_on: []const u64 = &.{},

    pub fn isOpen(self: Task) bool {
        return self.status.isOpen();
    }

    pub fn hasTag(self: Task, tag: []const u8) bool {
        for (self.tags) |existing| {
            if (std.ascii.eqlIgnoreCase(existing, tag)) return true;
        }
        return false;
    }

    pub fn matchesQuery(self: Task, query: []const u8) bool {
        if (query.len == 0) return true;
        return containsIgnoreCase(self.title, query) or
            (self.notes != null and containsIgnoreCase(self.notes.?, query)) or
            (self.blocked_reason != null and containsIgnoreCase(self.blocked_reason.?, query)) or
            tagsMatch(self.tags, query);
    }
};

pub const Project = struct {
    id: u64,
    name: []const u8,
    description: ?[]const u8 = null,
    status: ProjectStatus = .active,
    created_on: []const u8,
    updated_on: []const u8,
    archived_on: ?[]const u8 = null,
    deadline: ?[]const u8 = null,

    pub fn matchesQuery(self: Project, query: []const u8) bool {
        return containsIgnoreCase(self.name, query) or
            (self.description != null and containsIgnoreCase(self.description.?, query));
    }
};

pub const ProjectSummary = struct {
    total_tasks: usize = 0,
    open_tasks: usize = 0,
    completed_tasks: usize = 0,
    overdue_tasks: usize = 0,
    next_action_tasks: usize = 0,
    waiting_tasks: usize = 0,
    blocked_tasks: usize = 0,
    dependency_blocked_tasks: usize = 0,
    completion_percent: u8 = 0,
};

pub const NewTask = struct {
    title: []const u8,
    notes: ?[]const u8 = null,
    project_id: ?u64 = null,
    priority: Priority = .medium,
    tags: []const []const u8 = &.{},
    due_date: ?[]const u8 = null,
    recurrence: ?RecurrenceRule = null,
    waiting_until: ?[]const u8 = null,
    blocked_reason: ?[]const u8 = null,
    depends_on: []const u64 = &.{},
};

pub fn PatchOptional(comptime T: type) type {
    return union(enum) {
        absent,
        clear,
        set: T,
    };
}

pub const TaskPatch = struct {
    title: ?[]const u8 = null,
    notes: PatchOptional([]const u8) = .absent,
    project_id: PatchOptional(u64) = .absent,
    status: ?TaskStatus = null,
    priority: ?Priority = null,
    tags: ?[]const []const u8 = null,
    due_date: PatchOptional([]const u8) = .absent,
    recurrence: PatchOptional(RecurrenceRule) = .absent,
    waiting_until: PatchOptional([]const u8) = .absent,
    blocked_reason: PatchOptional([]const u8) = .absent,
    depends_on: ?[]const u64 = null,

    pub fn isEmpty(self: TaskPatch) bool {
        return self.title == null and self.notes == .absent and self.project_id == .absent and
            self.status == null and self.priority == null and self.tags == null and
            self.due_date == .absent and self.recurrence == .absent and self.waiting_until == .absent and
            self.blocked_reason == .absent and self.depends_on == null;
    }
};

pub const ProjectPatch = struct {
    description: PatchOptional([]const u8) = .absent,
    deadline: PatchOptional([]const u8) = .absent,

    pub fn isEmpty(self: ProjectPatch) bool {
        return self.description == .absent and self.deadline == .absent;
    }
};

pub const AppState = struct {
    allocator: std.mem.Allocator,
    schema_version: u32 = CURRENT_APP_SCHEMA_VERSION,
    next_task_id: u64 = 1,
    next_project_id: u64 = 1,
    tasks: std.ArrayList(Task) = .empty,
    projects: std.ArrayList(Project) = .empty,

    pub fn init(allocator: std.mem.Allocator) AppState {
        return .{ .allocator = allocator };
    }

    pub fn findTaskIndex(self: *const AppState, task_id: u64) ?usize {
        for (self.tasks.items, 0..) |task, index| {
            if (task.id == task_id) return index;
        }
        return null;
    }

    pub fn findProjectIndex(self: *const AppState, project_id: u64) ?usize {
        for (self.projects.items, 0..) |project, index| {
            if (project.id == project_id) return index;
        }
        return null;
    }

    pub fn findTask(self: *const AppState, task_id: u64) ?*const Task {
        const index = self.findTaskIndex(task_id) orelse return null;
        return &self.tasks.items[index];
    }

    pub fn findProject(self: *const AppState, project_id: u64) ?*const Project {
        const index = self.findProjectIndex(project_id) orelse return null;
        return &self.projects.items[index];
    }

    pub fn projectName(self: *const AppState, project_id: u64) ?[]const u8 {
        return if (self.findProject(project_id)) |project| project.name else null;
    }

    pub fn isProjectArchived(self: *const AppState, project_id: u64) bool {
        return if (self.findProject(project_id)) |project| project.status == .archived else false;
    }

    pub fn resolveProjectId(self: *const AppState, reference: []const u8) !u64 {
        const cleaned = std.mem.trim(u8, reference, " \t\r\n");
        if (cleaned.len == 0) return error.EmptyField;
        if (std.fmt.parseInt(u64, cleaned, 10)) |id| {
            if (self.findProject(id) != null) return id;
        } else |_| {}
        for (self.projects.items) |project| {
            if (std.ascii.eqlIgnoreCase(project.name, cleaned)) return project.id;
        }
        return error.ProjectNotFound;
    }

    pub fn createProject(self: *AppState, name: []const u8, description: ?[]const u8, deadline: ?[]const u8, today: []const u8) !Project {
        const cleaned = try cleanRequired(self.allocator, name);
        for (self.projects.items) |project| {
            if (std.ascii.eqlIgnoreCase(project.name, cleaned)) return error.DuplicateProject;
        }
        const project = Project{
            .id = self.next_project_id,
            .name = cleaned,
            .description = try cleanOptional(self.allocator, description),
            .status = .active,
            .created_on = try self.allocator.dupe(u8, today),
            .updated_on = try self.allocator.dupe(u8, today),
            .deadline = if (deadline) |value| try self.allocator.dupe(u8, value) else null,
        };
        self.next_project_id += 1;
        try self.projects.append(self.allocator, project);
        return project;
    }

    pub fn createTask(self: *AppState, input: NewTask, today: []const u8) !Task {
        const title = try cleanRequired(self.allocator, input.title);
        if (input.recurrence != null and input.due_date == null) return error.RecurrenceRequiresDueDate;
        if (input.project_id) |project_id| {
            if (self.findProject(project_id) == null) return error.ProjectNotFound;
        }
        const depends_on = try self.validateTaskDependencies(null, input.depends_on);
        const blocked_reason = try cleanOptional(self.allocator, input.blocked_reason);
        const waiting_until = if (input.waiting_until) |value| try self.allocator.dupe(u8, value) else null;
        const status: TaskStatus = if (blocked_reason != null) .blocked else if (waiting_until != null) .waiting else .todo;
        const task = Task{
            .id = self.next_task_id,
            .title = title,
            .notes = try cleanOptional(self.allocator, input.notes),
            .project_id = input.project_id,
            .status = status,
            .priority = input.priority,
            .tags = try normalizeTags(self.allocator, input.tags),
            .due_date = if (input.due_date) |value| try self.allocator.dupe(u8, value) else null,
            .recurrence = input.recurrence,
            .created_on = try self.allocator.dupe(u8, today),
            .updated_on = try self.allocator.dupe(u8, today),
            .waiting_until = waiting_until,
            .blocked_reason = blocked_reason,
            .depends_on = depends_on,
        };
        self.next_task_id += 1;
        try self.tasks.append(self.allocator, task);
        return task;
    }

    pub fn applyTaskPatch(self: *AppState, task_id: u64, patch: TaskPatch, today: []const u8) !void {
        if (patch.project_id == .set and self.findProject(patch.project_id.set) == null) return error.ProjectNotFound;
        const normalized_deps = if (patch.depends_on) |deps| try self.validateTaskDependencies(task_id, deps) else null;
        const index = self.findTaskIndex(task_id) orelse return error.TaskNotFound;
        var task = &self.tasks.items[index];
        if (patch.title) |title| task.title = try cleanRequired(self.allocator, title);
        switch (patch.notes) {
            .absent => {},
            .clear => task.notes = null,
            .set => |value| task.notes = try cleanOptional(self.allocator, value),
        }
        switch (patch.project_id) {
            .absent => {},
            .clear => task.project_id = null,
            .set => |value| task.project_id = value,
        }
        if (patch.priority) |value| task.priority = value;
        if (patch.tags) |value| task.tags = try normalizeTags(self.allocator, value);
        switch (patch.due_date) {
            .absent => {},
            .clear => task.due_date = null,
            .set => |value| task.due_date = try self.allocator.dupe(u8, value),
        }
        switch (patch.recurrence) {
            .absent => {},
            .clear => task.recurrence = null,
            .set => |value| task.recurrence = value,
        }
        switch (patch.waiting_until) {
            .absent => {},
            .clear => task.waiting_until = null,
            .set => |value| task.waiting_until = try self.allocator.dupe(u8, value),
        }
        switch (patch.blocked_reason) {
            .absent => {},
            .clear => task.blocked_reason = null,
            .set => |value| task.blocked_reason = try cleanOptional(self.allocator, value),
        }
        if (normalized_deps) |deps| task.depends_on = deps;
        if (patch.status) |status| task.status = status;
        if (task.recurrence != null and task.due_date == null) return error.RecurrenceRequiresDueDate;
        task.updated_on = try self.allocator.dupe(u8, today);
        if (task.status == .next_action) {
            if (task.project_id) |project_id| self.demoteOtherNextActions(project_id, task_id, today) catch return error.OutOfMemory;
        }
    }

    pub fn setTaskStatus(self: *AppState, task_id: u64, status: TaskStatus, today: []const u8) !?u64 {
        switch (status) {
            .done => return try self.completeTask(task_id, today),
            .next_action => {
                const index = self.findTaskIndex(task_id) orelse return error.TaskNotFound;
                var project_id: ?u64 = null;
                {
                    var task = &self.tasks.items[index];
                    task.status = .next_action;
                    task.completed_on = null;
                    task.archived_on = null;
                    task.waiting_until = null;
                    task.blocked_reason = null;
                    task.updated_on = try self.allocator.dupe(u8, today);
                    project_id = task.project_id;
                }
                if (project_id) |id| try self.demoteOtherNextActions(id, task_id, today);
                return null;
            },
            .todo, .in_progress, .waiting, .blocked => {
                const index = self.findTaskIndex(task_id) orelse return error.TaskNotFound;
                var task = &self.tasks.items[index];
                task.status = status;
                task.completed_on = null;
                task.archived_on = null;
                if (status != .waiting) task.waiting_until = null;
                if (status != .blocked) task.blocked_reason = null;
                task.updated_on = try self.allocator.dupe(u8, today);
                return null;
            },
            .archived => {
                const index = self.findTaskIndex(task_id) orelse return error.TaskNotFound;
                var task = &self.tasks.items[index];
                task.status = .archived;
                task.archived_on = try self.allocator.dupe(u8, today);
                task.waiting_until = null;
                task.blocked_reason = null;
                task.updated_on = try self.allocator.dupe(u8, today);
                return null;
            },
        }
    }

    pub fn completeTask(self: *AppState, task_id: u64, today: []const u8) !?u64 {
        const index = self.findTaskIndex(task_id) orelse return error.TaskNotFound;
        var spawn_template: ?Task = null;
        {
            var task = &self.tasks.items[index];
            if (!task.status.isOpen()) return error.TaskAlreadyClosed;
            task.status = .done;
            task.completed_on = try self.allocator.dupe(u8, today);
            task.waiting_until = null;
            task.blocked_reason = null;
            task.updated_on = try self.allocator.dupe(u8, today);
            if (task.recurrence != null) {
                if (task.due_date == null) return error.RecurrenceRequiresDueDate;
                spawn_template = task.*;
            }
        }
        if (spawn_template) |template| {
            const next_due = try template.recurrence.?.nextDueDate(template.due_date.?, self.allocator);
            const spawned = Task{
                .id = self.next_task_id,
                .title = try self.allocator.dupe(u8, template.title),
                .notes = if (template.notes) |value| try self.allocator.dupe(u8, value) else null,
                .project_id = template.project_id,
                .status = .todo,
                .priority = template.priority,
                .tags = try duplicateTags(self.allocator, template.tags),
                .due_date = next_due,
                .recurrence = template.recurrence,
                .created_on = try self.allocator.dupe(u8, today),
                .updated_on = try self.allocator.dupe(u8, today),
                .depends_on = &.{},
            };
            const spawned_id = spawned.id;
            self.next_task_id += 1;
            try self.tasks.append(self.allocator, spawned);
            return spawned_id;
        }
        return null;
    }

    pub fn deleteTask(self: *AppState, task_id: u64) !Task {
        const index = self.findTaskIndex(task_id) orelse return error.TaskNotFound;
        const removed = self.tasks.orderedRemove(index);
        for (self.tasks.items) |*task| {
            var deps: std.ArrayList(u64) = .empty;
            for (task.depends_on) |dep| {
                if (dep != task_id) try deps.append(self.allocator, dep);
            }
            task.depends_on = try deps.toOwnedSlice(self.allocator);
        }
        return removed;
    }

    pub fn archiveProject(self: *AppState, project_id: u64, today: []const u8) !void {
        const index = self.findProjectIndex(project_id) orelse return error.ProjectNotFound;
        var project = &self.projects.items[index];
        if (project.status == .archived) return error.ProjectAlreadyArchived;
        project.status = .archived;
        project.archived_on = try self.allocator.dupe(u8, today);
        project.updated_on = try self.allocator.dupe(u8, today);
    }

    pub fn activateProject(self: *AppState, project_id: u64, today: []const u8) !void {
        const index = self.findProjectIndex(project_id) orelse return error.ProjectNotFound;
        var project = &self.projects.items[index];
        if (project.status == .active) return error.ProjectAlreadyActive;
        project.status = .active;
        project.archived_on = null;
        project.updated_on = try self.allocator.dupe(u8, today);
    }

    pub fn applyProjectPatch(self: *AppState, project_id: u64, patch: ProjectPatch, today: []const u8) !void {
        const index = self.findProjectIndex(project_id) orelse return error.ProjectNotFound;
        var project = &self.projects.items[index];
        switch (patch.description) {
            .absent => {},
            .clear => project.description = null,
            .set => |value| project.description = try cleanOptional(self.allocator, value),
        }
        switch (patch.deadline) {
            .absent => {},
            .clear => project.deadline = null,
            .set => |value| project.deadline = try self.allocator.dupe(u8, value),
        }
        project.updated_on = try self.allocator.dupe(u8, today);
    }

    pub fn projectSummary(self: *const AppState, project_id: u64, today: []const u8) !ProjectSummary {
        if (self.findProject(project_id) == null) return error.ProjectNotFound;
        const today_date = dates.Date.parse(today) catch return error.InvalidDate;
        var summary = ProjectSummary{};
        for (self.tasks.items) |task| {
            if (task.project_id != project_id or task.status == .archived) continue;
            summary.total_tasks += 1;
            if (task.status == .done) summary.completed_tasks += 1;
            if (task.isOpen()) summary.open_tasks += 1;
            if (task.isOpen() and task.due_date != null) {
                const due = dates.Date.parse(task.due_date.?) catch null;
                if (due != null and due.?.compare(today_date) == .lt) summary.overdue_tasks += 1;
            }
            if (task.status.isNextAction()) summary.next_action_tasks += 1;
            if (task.status == .waiting) summary.waiting_tasks += 1;
            if (task.status == .blocked) summary.blocked_tasks += 1;
            if (task.isOpen() and self.hasUnresolvedDependencies(task)) summary.dependency_blocked_tasks += 1;
        }
        summary.completion_percent = if (summary.total_tasks == 0) 0 else @intCast((summary.completed_tasks * 100) / summary.total_tasks);
        return summary;
    }

    pub fn unresolvedTaskDependencies(self: *const AppState, task: Task, allocator: std.mem.Allocator) ![]const u64 {
        var deps: std.ArrayList(u64) = .empty;
        for (task.depends_on) |dep| {
            const dependency = self.findTask(dep);
            if (dependency == null or dependency.?.status.isOpen()) try deps.append(allocator, dep);
        }
        return deps.toOwnedSlice(allocator);
    }

    pub fn hasUnresolvedDependencies(self: *const AppState, task: Task) bool {
        for (task.depends_on) |dep| {
            const dependency = self.findTask(dep);
            if (dependency == null or dependency.?.status.isOpen()) return true;
        }
        return false;
    }

    pub fn taskInActiveProject(self: *const AppState, task: Task) bool {
        return if (task.project_id) |project_id| !self.isProjectArchived(project_id) else true;
    }

    fn validateTaskDependencies(self: *const AppState, task_id: ?u64, dependencies: []const u64) ![]const u64 {
        const normalized = try normalizeDependencies(self.allocator, dependencies);
        for (normalized) |dep| {
            if (self.findTask(dep) == null) return error.InvalidTaskDependency;
        }
        if (task_id) |id| {
            for (normalized) |dep| {
                if (dep == id or self.taskReachesTarget(dep, id, id, normalized)) return error.TaskDependencyCycle;
            }
        }
        return normalized;
    }

    fn demoteOtherNextActions(self: *AppState, project_id: u64, keep_task_id: u64, today: []const u8) !void {
        for (self.tasks.items) |*task| {
            if (task.id != keep_task_id and task.project_id == project_id and task.status == .next_action) {
                task.status = .todo;
                task.updated_on = try self.allocator.dupe(u8, today);
            }
        }
    }

    fn taskReachesTarget(self: *const AppState, start: u64, target: u64, patched_task_id: u64, patched_dependencies: []const u64) bool {
        var visited = std.AutoHashMap(u64, void).init(self.allocator);
        defer visited.deinit();
        return self.taskReachesTargetInner(start, target, patched_task_id, patched_dependencies, &visited) catch false;
    }

    fn taskReachesTargetInner(self: *const AppState, current: u64, target: u64, patched_task_id: u64, patched_dependencies: []const u64, visited: *std.AutoHashMap(u64, void)) !bool {
        if (visited.contains(current)) return false;
        try visited.put(current, {});
        const deps = if (current == patched_task_id) patched_dependencies else blk: {
            const task = self.findTask(current) orelse break :blk &[_]u64{};
            break :blk task.depends_on;
        };
        for (deps) |dep| {
            if (dep == target or try self.taskReachesTargetInner(dep, target, patched_task_id, patched_dependencies, visited)) return true;
        }
        return false;
    }
};

pub fn parsePriority(value: []const u8) ?Priority {
    inline for (@typeInfo(Priority).@"enum".fields) |field| {
        if (std.ascii.eqlIgnoreCase(value, field.name)) return @enumFromInt(field.value);
    }
    return null;
}

pub fn parseStatus(value: []const u8) ?TaskStatus {
    inline for (@typeInfo(TaskStatus).@"enum".fields) |field| {
        if (std.ascii.eqlIgnoreCase(value, field.name)) return @enumFromInt(field.value);
    }
    return null;
}

pub fn parseRecurrence(value: []const u8) ?RecurrenceRule {
    inline for (@typeInfo(RecurrenceRule).@"enum".fields) |field| {
        if (std.ascii.eqlIgnoreCase(value, field.name)) return @enumFromInt(field.value);
    }
    return null;
}

pub fn normalizeTags(allocator: std.mem.Allocator, tags: []const []const u8) ![]const []const u8 {
    var normalized: std.ArrayList([]const u8) = .empty;
    for (tags) |tag| {
        const cleaned_raw = std.mem.trim(u8, tag, " \t\r\n#");
        if (cleaned_raw.len == 0) continue;
        const cleaned = try asciiLowerAlloc(allocator, cleaned_raw);
        var exists = false;
        for (normalized.items) |existing| {
            if (std.mem.eql(u8, existing, cleaned)) {
                exists = true;
                break;
            }
        }
        if (!exists) try normalized.append(allocator, cleaned);
    }
    return normalized.toOwnedSlice(allocator);
}

pub fn duplicateTags(allocator: std.mem.Allocator, tags: []const []const u8) ![]const []const u8 {
    var out: std.ArrayList([]const u8) = .empty;
    for (tags) |tag| try out.append(allocator, try allocator.dupe(u8, tag));
    return out.toOwnedSlice(allocator);
}

fn normalizeDependencies(allocator: std.mem.Allocator, deps: []const u64) ![]const u64 {
    var out: std.ArrayList(u64) = .empty;
    for (deps) |dep| {
        var exists = false;
        for (out.items) |existing| {
            if (existing == dep) {
                exists = true;
                break;
            }
        }
        if (!exists) try out.append(allocator, dep);
    }
    std.mem.sort(u64, out.items, {}, std.sort.asc(u64));
    return out.toOwnedSlice(allocator);
}

fn cleanRequired(allocator: std.mem.Allocator, raw: []const u8) ![]const u8 {
    const cleaned = std.mem.trim(u8, raw, " \t\r\n");
    if (cleaned.len == 0) return error.EmptyField;
    return allocator.dupe(u8, cleaned);
}

fn cleanOptional(allocator: std.mem.Allocator, raw: ?[]const u8) !?[]const u8 {
    const value = raw orelse return null;
    const cleaned = std.mem.trim(u8, value, " \t\r\n");
    if (cleaned.len == 0) return null;
    const copy: []const u8 = try allocator.dupe(u8, cleaned);
    return copy;
}

fn asciiLowerAlloc(allocator: std.mem.Allocator, value: []const u8) ![]const u8 {
    const out = try allocator.alloc(u8, value.len);
    for (value, 0..) |c, i| out[i] = std.ascii.toLower(c);
    return out;
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

fn tagsMatch(tags: []const []const u8, query: []const u8) bool {
    for (tags) |tag| {
        if (containsIgnoreCase(tag, query)) return true;
    }
    return false;
}

test "recurring completion spawns next task" {
    var arena = std.heap.ArenaAllocator.init(std.testing.allocator);
    defer arena.deinit();
    var state = AppState.init(arena.allocator());
    _ = try state.createTask(.{
        .title = "Write report",
        .priority = .high,
        .due_date = "2026-03-14",
        .recurrence = .weekly,
    }, "2026-03-14");
    const spawned = try state.completeTask(1, "2026-03-14");
    try std.testing.expectEqual(@as(?u64, 2), spawned);
    try std.testing.expectEqual(TaskStatus.done, state.tasks.items[0].status);
    try std.testing.expectEqualStrings("2026-03-21", state.tasks.items[1].due_date.?);
}
