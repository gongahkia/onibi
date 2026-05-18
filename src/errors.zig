const std = @import("std");

pub const ErrorCategory = enum {
    usage,
    not_found,
    conflict,
    storage,

    pub fn exitCode(self: ErrorCategory) i32 {
        return switch (self) {
            .usage => 2,
            .not_found => 3,
            .conflict => 4,
            .storage => 5,
        };
    }
};

pub const ErrorReport = struct {
    exit_code: i32,
    code: []const u8,
    message: []const u8,
    details: []const []const u8 = &.{},
};

pub fn report(category: ErrorCategory, code: []const u8, message: []const u8) ErrorReport {
    return .{ .exit_code = category.exitCode(), .code = code, .message = message };
}

pub fn usage(code: []const u8, message: []const u8) ErrorReport {
    return report(.usage, code, message);
}

pub fn notFound(code: []const u8, message: []const u8) ErrorReport {
    return report(.not_found, code, message);
}

pub fn conflict(code: []const u8, message: []const u8) ErrorReport {
    return report(.conflict, code, message);
}

pub fn storage(message: []const u8) ErrorReport {
    return report(.storage, "storage_error", message);
}

pub fn fromDomain(allocator: std.mem.Allocator, err: anyerror, subject: []const u8) ErrorReport {
    _ = allocator;
    return switch (err) {
        error.TaskNotFound => notFound("task_not_found", subject),
        error.ProjectNotFound => notFound("project_not_found", subject),
        error.InvalidTaskDependency => notFound("task_dependency_not_found", subject),
        error.DuplicateProject => conflict("duplicate_project", subject),
        error.EmptyField => usage("empty_field", subject),
        error.RecurrenceRequiresDueDate => usage("recurrence_requires_due_date", "recurring tasks require a due date"),
        error.TaskAlreadyClosed => conflict("task_already_closed", subject),
        error.ProjectAlreadyArchived => conflict("project_already_archived", subject),
        error.ProjectAlreadyActive => conflict("project_already_active", subject),
        error.TaskDependencyCycle => conflict("task_dependency_cycle", subject),
        else => storage(@errorName(err)),
    };
}
