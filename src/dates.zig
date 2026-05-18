const std = @import("std");

pub const Date = struct {
    year: i32,
    month: u8,
    day: u8,

    pub fn parse(raw: []const u8) !Date {
        const value = std.mem.trim(u8, raw, " \t\r\n");
        if (value.len != 10 or value[4] != '-' or value[7] != '-') return error.InvalidDate;
        const year = try std.fmt.parseInt(i32, value[0..4], 10);
        const month = try std.fmt.parseInt(u8, value[5..7], 10);
        const day = try std.fmt.parseInt(u8, value[8..10], 10);
        if (month < 1 or month > 12) return error.InvalidDate;
        if (day < 1 or day > daysInMonth(year, month)) return error.InvalidDate;
        return .{ .year = year, .month = month, .day = day };
    }

    pub fn formatAlloc(self: Date, allocator: std.mem.Allocator) ![]const u8 {
        return std.fmt.allocPrint(allocator, "{d:0>4}-{d:0>2}-{d:0>2}", .{ @as(u32, @intCast(self.year)), self.month, self.day });
    }

    pub fn compare(self: Date, other: Date) std.math.Order {
        const left = self.toEpochDay();
        const right = other.toEpochDay();
        if (left < right) return .lt;
        if (left > right) return .gt;
        return .eq;
    }

    pub fn addDays(self: Date, days: i64) Date {
        return fromEpochDay(self.toEpochDay() + days);
    }

    pub fn addOneMonth(self: Date) Date {
        var year = self.year;
        var month: u8 = self.month + 1;
        if (month == 13) {
            year += 1;
            month = 1;
        }
        const day = @min(self.day, daysInMonth(year, month));
        return .{ .year = year, .month = month, .day = day };
    }

    pub fn weekdayMonday0(self: Date) u8 {
        const z = self.toEpochDay();
        return @intCast(@mod(z + 3, 7));
    }

    pub fn toEpochDay(self: Date) i64 {
        var y = self.year;
        const m: i32 = self.month;
        const d: i32 = self.day;
        y -= if (m <= 2) 1 else 0;
        const era = @divFloor(y, 400);
        const yoe = y - era * 400;
        const mp = m + if (m > 2) @as(i32, -3) else @as(i32, 9);
        const doy = @divFloor(153 * mp + 2, 5) + d - 1;
        const doe = yoe * 365 + @divFloor(yoe, 4) - @divFloor(yoe, 100) + doy;
        return @as(i64, era) * 146097 + doe - 719468;
    }
};

pub fn todayUtc() Date {
    const seconds = std.time.timestamp();
    return fromEpochSeconds(seconds);
}

pub fn resolveExpression(allocator: std.mem.Allocator, today: Date, raw: []const u8) !Date {
    _ = allocator;
    const trimmed = std.mem.trim(u8, raw, " \t\r\n");
    if (trimmed.len == 0) return error.EmptyDateExpression;

    var normalized_buf: [128]u8 = undefined;
    if (trimmed.len >= normalized_buf.len) return error.InvalidDateExpression;
    for (trimmed, 0..) |c, i| normalized_buf[i] = std.ascii.toLower(if (c == '_') '-' else c);
    const normalized = normalized_buf[0..trimmed.len];

    if (normalized[0] == '+') {
        const suffix = if (std.mem.endsWith(u8, normalized, "d")) normalized[1 .. normalized.len - 1] else normalized[1..];
        if (suffix.len == 0) return error.InvalidRelativeDateExpression;
        const days = try std.fmt.parseInt(i64, suffix, 10);
        if (days < 0) return error.InvalidRelativeDateExpression;
        return today.addDays(days);
    }

    if (resolveWeekday(today, normalized)) |date| return date;
    if (std.mem.eql(u8, normalized, "today")) return today;
    if (std.mem.eql(u8, normalized, "tomorrow")) return today.addDays(1);
    if (std.mem.eql(u8, normalized, "next-week")) return today.addDays(7);
    if (std.mem.eql(u8, normalized, "next-month")) return today.addOneMonth();
    return Date.parse(trimmed) catch error.InvalidDateExpression;
}

pub fn addOneMonth(date: Date) Date {
    return date.addOneMonth();
}

pub fn fromEpochDay(days: i64) Date {
    const z = days + 719468;
    const era = @divFloor(z, 146097);
    const doe: i64 = z - era * 146097;
    const yoe: i64 = @divFloor(doe - @divFloor(doe, 1460) + @divFloor(doe, 36524) - @divFloor(doe, 146096), 365);
    var y: i64 = yoe + era * 400;
    const doy: i64 = doe - (365 * yoe + @divFloor(yoe, 4) - @divFloor(yoe, 100));
    const mp: i64 = @divFloor(5 * doy + 2, 153);
    const d: i64 = doy - @divFloor(153 * mp + 2, 5) + 1;
    const m: i64 = mp + if (mp < 10) @as(i64, 3) else @as(i64, -9);
    y += if (m <= 2) 1 else 0;
    return .{ .year = @intCast(y), .month = @intCast(m), .day = @intCast(d) };
}

pub fn fromEpochSeconds(seconds: i64) Date {
    return fromEpochDay(@divFloor(seconds, 86_400));
}

fn resolveWeekday(today: Date, value: []const u8) ?Date {
    var name = value;
    var force_next_week = false;
    if (std.mem.startsWith(u8, value, "next-")) {
        name = value[5..];
        force_next_week = true;
    }
    const target = parseWeekday(name) orelse return null;
    const current = today.weekdayMonday0();
    var delta = @mod(@as(i32, target) - @as(i32, current), 7);
    if (force_next_week and delta == 0) delta = 7;
    return today.addDays(delta);
}

fn parseWeekday(value: []const u8) ?u8 {
    if (std.mem.eql(u8, value, "mon") or std.mem.eql(u8, value, "monday")) return 0;
    if (std.mem.eql(u8, value, "tue") or std.mem.eql(u8, value, "tues") or std.mem.eql(u8, value, "tuesday")) return 1;
    if (std.mem.eql(u8, value, "wed") or std.mem.eql(u8, value, "wednesday")) return 2;
    if (std.mem.eql(u8, value, "thu") or std.mem.eql(u8, value, "thur") or std.mem.eql(u8, value, "thurs") or std.mem.eql(u8, value, "thursday")) return 3;
    if (std.mem.eql(u8, value, "fri") or std.mem.eql(u8, value, "friday")) return 4;
    if (std.mem.eql(u8, value, "sat") or std.mem.eql(u8, value, "saturday")) return 5;
    if (std.mem.eql(u8, value, "sun") or std.mem.eql(u8, value, "sunday")) return 6;
    return null;
}

pub fn daysInMonth(year: i32, month: u8) u8 {
    return switch (month) {
        1, 3, 5, 7, 8, 10, 12 => 31,
        4, 6, 9, 11 => 30,
        2 => if (isLeapYear(year)) 29 else 28,
        else => 0,
    };
}

fn isLeapYear(year: i32) bool {
    return @mod(year, 4) == 0 and (@mod(year, 100) != 0 or @mod(year, 400) == 0);
}

test "date expressions match Kelp shortcuts" {
    const today = try Date.parse("2026-03-14");
    try std.testing.expectEqual(Date{ .year = 2026, .month = 3, .day = 15 }, try resolveExpression(std.testing.allocator, today, "tomorrow"));
    try std.testing.expectEqual(Date{ .year = 2026, .month = 3, .day = 17 }, try resolveExpression(std.testing.allocator, today, "+3d"));
    try std.testing.expectEqual(Date{ .year = 2026, .month = 3, .day = 16 }, try resolveExpression(std.testing.allocator, today, "next-monday"));
}
