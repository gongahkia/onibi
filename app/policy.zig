const std = @import("std");
const common = @import("common.zig");

pub const Action = enum {
    allow,
    log_only,
    require_approval,
    deny,

    fn rank(self: Action) u8 {
        return switch (self) {
            .allow => 0,
            .log_only => 1,
            .require_approval => 2,
            .deny => 3,
        };
    }

    fn text(self: Action) []const u8 {
        return switch (self) {
            .allow => "allow",
            .log_only => "log-only",
            .require_approval => "require-approval",
            .deny => "deny",
        };
    }
};

pub const StringList = struct {
    items: [64][]const u8 = undefined,
    len: usize = 0,

    fn push(self: *StringList, value: []const u8) void {
        if (self.len < self.items.len) {
            self.items[self.len] = value;
            self.len += 1;
        }
    }

    fn anyIn(self: StringList, haystack: []const u8) bool {
        var index: usize = 0;
        while (index < self.len) : (index += 1) {
            if (common.containsIgnoreCase(haystack, self.items[index])) return true;
        }
        return false;
    }
};

const Rule = struct {
    id: []const u8 = "",
    tool: []const u8 = "",
    command_any: StringList = .{},
    command_any_secondary: StringList = .{},
    action: Action = .allow,
    approver_role: []const u8 = "",

    fn matches(self: Rule, tool: []const u8, command: []const u8) bool {
        if (self.tool.len != 0 and !std.mem.eql(u8, self.tool, tool)) return false;
        if (self.command_any.len != 0 and !self.command_any.anyIn(command)) return false;
        if (self.command_any_secondary.len != 0 and !self.command_any_secondary.anyIn(command)) return false;
        return true;
    }
};

pub const RuleSet = struct {
    rules: [64]Rule = undefined,
    len: usize = 0,

    fn push(self: *RuleSet, rule: Rule) void {
        if (self.len < self.rules.len) {
            self.rules[self.len] = rule;
            self.len += 1;
        }
    }
};

pub const Decision = struct {
    action: Action,
    selected_rule: []const u8,
    matched: StringList,
    approver_role: []const u8,
};

pub fn policyCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "check")) return common.fail("usage: kelp-pi policy check --tool TOOL --command CMD", 64);
    const tool = common.option(args[1..], "--tool") orelse "Bash";
    const command = common.option(args[1..], "--command") orelse "";
    const policy_path = common.option(args[1..], "--policy") orelse common.default_policy_path;
    const policy_text = try std.fs.cwd().readFileAlloc(allocator, policy_path, 1024 * 1024);
    defer allocator.free(policy_text);
    return printDecision(evaluatePolicy(parseRules(policy_text), tool, command));
}

pub fn parseRules(text: []const u8) RuleSet {
    var rules = RuleSet{};
    var current = Rule{};
    var in_rule = false;
    var lines = std.mem.splitScalar(u8, text, '\n');
    while (lines.next()) |raw| {
        const line = std.mem.trim(u8, raw, " \t\r");
        if (line.len == 0 or line[0] == '#') continue;
        if (std.mem.eql(u8, line, "[[rule]]")) {
            if (in_rule) rules.push(current);
            current = Rule{};
            in_rule = true;
            continue;
        }
        if (common.startsWith(line, "id =")) current.id = common.quotedValue(line) orelse "";
        if (common.startsWith(line, "tool =")) current.tool = common.quotedValue(line) orelse "";
        if (common.startsWith(line, "action =")) current.action = parseAction(common.quotedValue(line) orelse "allow");
        if (common.startsWith(line, "approver_role =")) current.approver_role = common.quotedValue(line) orelse "";
        if (common.startsWith(line, "command_any =")) parseList(line, &current.command_any);
        if (common.startsWith(line, "command_any_secondary =")) parseList(line, &current.command_any_secondary);
    }
    if (in_rule) rules.push(current);
    return rules;
}

pub fn evaluatePolicy(rules: RuleSet, tool: []const u8, command: []const u8) Decision {
    var decision = Decision{ .action = .allow, .selected_rule = "", .matched = .{}, .approver_role = "" };
    var index: usize = 0;
    while (index < rules.len) : (index += 1) {
        const rule = rules.rules[index];
        if (!rule.matches(tool, command)) continue;
        decision.matched.push(rule.id);
        if (rule.action.rank() > decision.action.rank() or (rule.action.rank() == decision.action.rank() and decision.selected_rule.len == 0)) {
            decision.action = rule.action;
            decision.selected_rule = rule.id;
            decision.approver_role = rule.approver_role;
        }
    }
    return decision;
}

pub fn printDecision(decision: Decision) !void {
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"action\":\"{s}\",\"selectedRule\":\"{s}\",\"matchedRuleIds\":[", .{ decision.action.text(), decision.selected_rule });
    var index: usize = 0;
    while (index < decision.matched.len) : (index += 1) {
        if (index != 0) try out.print(",", .{});
        try out.print("\"{s}\"", .{decision.matched.items[index]});
    }
    try out.print("]", .{});
    if (decision.approver_role.len != 0) try out.print(",\"approverRole\":\"{s}\"", .{decision.approver_role});
    try out.print("}}\n", .{});
}

fn parseAction(value: []const u8) Action {
    if (std.mem.eql(u8, value, "deny")) return .deny;
    if (std.mem.eql(u8, value, "require-approval")) return .require_approval;
    if (std.mem.eql(u8, value, "log-only")) return .log_only;
    return .allow;
}

fn parseList(line: []const u8, list: *StringList) void {
    var rest = line;
    while (std.mem.indexOfScalar(u8, rest, '"')) |start| {
        const after = rest[start + 1 ..];
        const end = std.mem.indexOfScalar(u8, after, '"') orelse break;
        list.push(after[0..end]);
        rest = after[end + 1 ..];
    }
}

test "policy denies exploit and requires scanner approval" {
    const rules = parseRules(
        \\[[rule]]
        \\id = "appsec-agent-deny-exploit-execution"
        \\tool = "Bash"
        \\command_any = ["sqlmap"]
        \\action = "deny"
        \\
        \\[[rule]]
        \\id = "appsec-agent-review-active-scanner"
        \\tool = "Bash"
        \\command_any = ["nuclei"]
        \\action = "require-approval"
        \\approver_role = "appsec-reviewer"
    );
    try std.testing.expectEqual(Action.deny, evaluatePolicy(rules, "Bash", "sqlmap -u http://target").action);
    try std.testing.expectEqual(Action.require_approval, evaluatePolicy(rules, "Bash", "nuclei -u http://target").action);
}

test "deny outranks log-only" {
    const rules = parseRules(
        \\[[rule]]
        \\id = "log"
        \\tool = "Bash"
        \\command_any = ["docker build"]
        \\action = "log-only"
        \\
        \\[[rule]]
        \\id = "deny"
        \\tool = "Bash"
        \\command_any = ["rm -rf"]
        \\action = "deny"
    );
    const decision = evaluatePolicy(rules, "Bash", "docker build . && rm -rf /tmp/x");
    try std.testing.expectEqual(Action.deny, decision.action);
}

test "manifest parser primitives find quoted values and lists" {
    var list = StringList{};
    parseList("command_any = [\"nmap\", \"nuclei\"]", &list);
    try std.testing.expectEqual(@as(usize, 2), list.len);
    try std.testing.expect(list.anyIn("run nuclei"));
    try std.testing.expect(std.mem.eql(u8, common.quotedValue("id = \"abc\"").?, "abc"));
}

test "malicious evidence cannot bypass policy tool gates" {
    const policy = try std.fs.cwd().readFileAlloc(std.testing.allocator, "policies/appsec-agent-baseline.toml", 64 * 1024);
    defer std.testing.allocator.free(policy);
    const destructive = try std.fs.cwd().readFileAlloc(std.testing.allocator, "fixtures/adversarial-injections/direct-imperative/rm-rf-source.md", 64 * 1024);
    defer std.testing.allocator.free(destructive);
    const prompt = try std.fs.cwd().readFileAlloc(std.testing.allocator, "fixtures/adversarial-injections/prompt-injection/override-system-instructions.md", 64 * 1024);
    defer std.testing.allocator.free(prompt);
    const scanner = try std.mem.concat(std.testing.allocator, u8, &.{ "nuclei http://fixture.local\n", prompt });
    defer std.testing.allocator.free(scanner);
    const rules = parseRules(policy);
    try std.testing.expectEqual(Action.deny, evaluatePolicy(rules, "Bash", destructive).action);
    try std.testing.expectEqual(Action.require_approval, evaluatePolicy(rules, "Bash", scanner).action);
}
