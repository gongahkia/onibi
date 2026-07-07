const std = @import("std");
const common = @import("common.zig");
const sqlite = @cImport({
    @cInclude("sqlite3.h");
});

const Citation = struct {
    path: []const u8,
    chunk_id: []const u8,
    start_byte: i64,
    end_byte: i64,
    content: []const u8,
};

pub fn indexCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0 or !std.mem.eql(u8, args[0], "ingest")) return common.fail("usage: kelp-pi index ingest --input PATH [--path PATH]", 64);
    const input = common.option(args[1..], "--input") orelse return common.fail("index ingest requires --input", 64);
    const logical = common.option(args[1..], "--path") orelse input;
    const data_dir = common.option(args[1..], "--data-dir") orelse common.default_data_dir;
    const content = try std.fs.cwd().readFileAlloc(allocator, input, 16 * 1024 * 1024);
    defer allocator.free(content);
    if (binaryIngestRefused(content)) return common.fail("binary ingest refused", 77);
    const index_dir = try common.pathJoin(allocator, data_dir, "index");
    defer allocator.free(index_dir);
    try std.fs.cwd().makePath(index_dir);
    const db_path = try common.pathJoin(allocator, index_dir, "chunks.sqlite3");
    defer allocator.free(db_path);
    const chunk_hash = try common.contentHashHex(allocator, content);
    defer allocator.free(chunk_hash);
    const chunk_id = try std.fmt.allocPrint(allocator, "{s}:{s}", .{ logical, chunk_hash[0..16] });
    defer allocator.free(chunk_id);
    const db = try sqliteOpen(db_path);
    defer _ = sqlite.sqlite3_close(db);
    try applyIndexSchema(db);
    const outcome = try ingestChunk(allocator, db, logical, chunk_id, chunk_hash, content);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"ok\":true,\"path\":\"{s}\",\"chunkId\":\"{s}\",\"outcome\":\"{s}\",\"db\":\"{s}\"}}\n", .{ logical, chunk_id, outcome, db_path });
}

pub fn askCommand(allocator: std.mem.Allocator, args: []const []const u8) !void {
    if (args.len == 0) return common.fail("usage: kelp-pi ask QUERY [--data-dir DIR] [--top-k N] [--emit-finding]", 64);
    const query = args[0];
    const data_dir = common.option(args[1..], "--data-dir") orelse common.default_data_dir;
    const top_k = common.parseUsize(common.option(args[1..], "--top-k") orelse "3", 3);
    const emit_finding = common.hasFlag(args[1..], "--emit-finding");
    const finding_title = common.option(args[1..], "--finding-title") orelse query;
    const db_path = try common.pathJoin3(allocator, data_dir, "index", "chunks.sqlite3");
    defer allocator.free(db_path);
    if (!common.fileExists(db_path)) {
        return printNoAnswer(query, top_k, "no index", emit_finding);
    }
    const db = try sqliteOpen(db_path);
    defer _ = sqlite.sqlite3_close(db);
    try applyIndexSchema(db);
    const citations = try collectCitations(allocator, db, query, top_k);
    defer freeCitations(allocator, citations);
    try printAskResponse(allocator, query, top_k, citations, emit_finding, finding_title);
}

fn sqliteOpen(path: []const u8) !*sqlite.sqlite3 {
    var db: ?*sqlite.sqlite3 = null;
    var path_buf: [4096:0]u8 = undefined;
    const zpath = try std.fmt.bufPrintZ(&path_buf, "{s}", .{path});
    if (sqlite.sqlite3_open(zpath.ptr, &db) != sqlite.SQLITE_OK) return error.SqliteOpen;
    return db.?;
}

fn sqliteExec(db: *sqlite.sqlite3, sql_text: []const u8) !void {
    var sql_buf: [4096:0]u8 = undefined;
    const zsql = try std.fmt.bufPrintZ(&sql_buf, "{s}", .{sql_text});
    if (sqlite.sqlite3_exec(db, zsql.ptr, null, null, null) != sqlite.SQLITE_OK) return error.SqliteExec;
}

fn applyIndexSchema(db: *sqlite.sqlite3) !void {
    try sqliteExec(db,
        \\CREATE TABLE IF NOT EXISTS chunks (
        \\ id TEXT PRIMARY KEY,
        \\ path TEXT NOT NULL,
        \\ heading_path TEXT NOT NULL,
        \\ start_byte INTEGER NOT NULL CHECK (start_byte >= 0),
        \\ end_byte INTEGER NOT NULL CHECK (end_byte >= start_byte),
        \\ content_hash TEXT NOT NULL,
        \\ content TEXT NOT NULL,
        \\ ingested_at TEXT NOT NULL
        \\);
        \\CREATE TABLE IF NOT EXISTS source_files (
        \\ path TEXT PRIMARY KEY,
        \\ content_hash TEXT NOT NULL,
        \\ mtime_unix_nanos INTEGER NOT NULL CHECK (mtime_unix_nanos >= 0),
        \\ size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
        \\ chunk_count INTEGER NOT NULL CHECK (chunk_count >= 0),
        \\ ingested_at TEXT NOT NULL
        \\);
        \\CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(content, content='chunks', content_rowid='rowid');
    );
}

fn ingestChunk(allocator: std.mem.Allocator, db: *sqlite.sqlite3, path: []const u8, chunk_id: []const u8, hash: []const u8, content: []const u8) ![]const u8 {
    const existing = try sourceHash(allocator, db, path);
    if (existing) |existing_hash| {
        defer allocator.free(existing_hash);
        if (std.mem.eql(u8, existing_hash, hash)) return "unchanged";
    }
    try sqliteExec(db, "BEGIN IMMEDIATE");
    try execDeletePath(db, "DELETE FROM chunks WHERE path = ?1", path);
    try execInsertChunk(db, path, chunk_id, hash, content);
    try execUpsertSource(db, path, hash, content.len);
    try sqliteExec(db, "INSERT INTO chunks_fts(chunks_fts) VALUES('rebuild')");
    try sqliteExec(db, "COMMIT");
    return "replaced";
}

fn binaryIngestRefused(content: []const u8) bool {
    return std.mem.indexOfScalar(u8, content, 0) != null;
}

fn prepare(db: *sqlite.sqlite3, sql_text: []const u8) !*sqlite.sqlite3_stmt {
    var stmt: ?*sqlite.sqlite3_stmt = null;
    var sql_buf: [2048:0]u8 = undefined;
    const zsql = try std.fmt.bufPrintZ(&sql_buf, "{s}", .{sql_text});
    if (sqlite.sqlite3_prepare_v2(db, zsql.ptr, -1, &stmt, null) != sqlite.SQLITE_OK) return error.SqlitePrepare;
    return stmt.?;
}

fn bindText(stmt: *sqlite.sqlite3_stmt, index: c_int, value: []const u8) !void {
    if (sqlite.sqlite3_bind_text(stmt, index, value.ptr, @intCast(value.len), null) != sqlite.SQLITE_OK) return error.SqliteBind;
}

fn sourceHash(allocator: std.mem.Allocator, db: *sqlite.sqlite3, path: []const u8) !?[]u8 {
    const stmt = try prepare(db, "SELECT content_hash FROM source_files WHERE path = ?1");
    defer _ = sqlite.sqlite3_finalize(stmt);
    try bindText(stmt, 1, path);
    const rc = sqlite.sqlite3_step(stmt);
    if (rc == sqlite.SQLITE_ROW) {
        const text = sqlite.sqlite3_column_text(stmt, 0);
        if (text == null) return null;
        const len = sqlite.sqlite3_column_bytes(stmt, 0);
        return try allocator.dupe(u8, text[0..@intCast(len)]);
    }
    if (rc == sqlite.SQLITE_DONE) return null;
    return error.SqliteStep;
}

fn execDeletePath(db: *sqlite.sqlite3, sql_text: []const u8, path: []const u8) !void {
    const stmt = try prepare(db, sql_text);
    defer _ = sqlite.sqlite3_finalize(stmt);
    try bindText(stmt, 1, path);
    if (sqlite.sqlite3_step(stmt) != sqlite.SQLITE_DONE) return error.SqliteStep;
}

fn execInsertChunk(db: *sqlite.sqlite3, path: []const u8, chunk_id: []const u8, hash: []const u8, content: []const u8) !void {
    const stmt = try prepare(db, "INSERT INTO chunks (id, path, heading_path, start_byte, end_byte, content_hash, content, ingested_at) VALUES (?1, ?2, '[]', 0, ?3, ?4, ?5, datetime('now'))");
    defer _ = sqlite.sqlite3_finalize(stmt);
    try bindText(stmt, 1, chunk_id);
    try bindText(stmt, 2, path);
    if (sqlite.sqlite3_bind_int64(stmt, 3, @intCast(content.len)) != sqlite.SQLITE_OK) return error.SqliteBind;
    try bindText(stmt, 4, hash);
    try bindText(stmt, 5, content);
    if (sqlite.sqlite3_step(stmt) != sqlite.SQLITE_DONE) return error.SqliteStep;
}

fn execUpsertSource(db: *sqlite.sqlite3, path: []const u8, hash: []const u8, size: usize) !void {
    const stmt = try prepare(db, "INSERT INTO source_files (path, content_hash, mtime_unix_nanos, size_bytes, chunk_count, ingested_at) VALUES (?1, ?2, 0, ?3, 1, datetime('now')) ON CONFLICT(path) DO UPDATE SET content_hash = excluded.content_hash, size_bytes = excluded.size_bytes, chunk_count = excluded.chunk_count, ingested_at = excluded.ingested_at");
    defer _ = sqlite.sqlite3_finalize(stmt);
    try bindText(stmt, 1, path);
    try bindText(stmt, 2, hash);
    if (sqlite.sqlite3_bind_int64(stmt, 3, @intCast(size)) != sqlite.SQLITE_OK) return error.SqliteBind;
    if (sqlite.sqlite3_step(stmt) != sqlite.SQLITE_DONE) return error.SqliteStep;
}

fn collectCitations(allocator: std.mem.Allocator, db: *sqlite.sqlite3, query: []const u8, top_k: usize) ![]Citation {
    const stmt = try prepare(db,
        \\SELECT chunks.id, chunks.path, chunks.heading_path, chunks.start_byte, chunks.end_byte, -bm25(chunks_fts) AS score, chunks.content
        \\FROM chunks_fts JOIN chunks ON chunks_fts.rowid = chunks.rowid
        \\WHERE chunks_fts MATCH ?1 ORDER BY score DESC LIMIT ?2
    );
    defer _ = sqlite.sqlite3_finalize(stmt);
    try bindText(stmt, 1, query);
    if (sqlite.sqlite3_bind_int64(stmt, 2, @intCast(@max(top_k, 1))) != sqlite.SQLITE_OK) return error.SqliteBind;
    var citations: std.ArrayList(Citation) = .empty;
    errdefer freeCitations(allocator, citations.items);
    while (true) {
        const rc = sqlite.sqlite3_step(stmt);
        if (rc == sqlite.SQLITE_DONE) break;
        if (rc != sqlite.SQLITE_ROW) return error.SqliteStep;
        const id = columnText(stmt, 0);
        const path = columnText(stmt, 1);
        const path_copy = try allocator.dupe(u8, path);
        errdefer allocator.free(path_copy);
        const id_copy = try allocator.dupe(u8, id);
        errdefer allocator.free(id_copy);
        const content = columnText(stmt, 6);
        const content_copy = try allocator.dupe(u8, content);
        errdefer allocator.free(content_copy);
        try citations.append(allocator, .{
            .path = path_copy,
            .chunk_id = id_copy,
            .start_byte = sqlite.sqlite3_column_int64(stmt, 3),
            .end_byte = sqlite.sqlite3_column_int64(stmt, 4),
            .content = content_copy,
        });
    }
    return citations.toOwnedSlice(allocator);
}

fn printAskResponse(allocator: std.mem.Allocator, query: []const u8, top_k: usize, citations: []const Citation, emit_finding: bool, finding_title: []const u8) !void {
    if (citations.len == 0) return printNoAnswer(query, top_k, "no matching chunks", emit_finding);
    const answer = try buildCitedAnswerJson(allocator, query, citations);
    defer allocator.free(answer);
    var out = std.fs.File.stdout().deprecatedWriter();
    try out.print("{{\"query\":\"", .{});
    try common.writeJsonEscaped(&out, query);
    try out.print("\",\"topK\":{},\"noAnswer\":null,\"answer\":{s},\"citations\":[", .{ @max(top_k, 1), answer });
    try writeCitations(&out, citations);
    try out.writeAll("],\"results\":[]");
    if (emit_finding) {
        const finding = try buildGeneratedFindingJson(allocator, finding_title, citations);
        defer allocator.free(finding);
        try out.print(",\"generatedFindings\":[{s}]", .{finding});
    }
    try out.writeAll("}\n");
}

fn printNoAnswer(query: []const u8, top_k: usize, reason: []const u8, emit_finding: bool) !void {
    var out = std.fs.File.stdout().deprecatedWriter();
    if (emit_finding) {
        try out.print("{{\"ok\":false,\"query\":\"", .{});
        try common.writeJsonEscaped(&out, query);
        try out.print("\",\"topK\":{},\"reason\":\"generated finding requires at least one citation\",\"citations\":[],\"generatedFindings\":[]}}\n", .{@max(top_k, 1)});
    } else {
        try out.print("{{\"query\":\"", .{});
        try common.writeJsonEscaped(&out, query);
        try out.print("\",\"topK\":{},\"noAnswer\":{{\"reason\":\"{s}\",\"threshold\":{},\"maxScore\":null}},\"citations\":[],\"results\":[]}}\n", .{ @max(top_k, 1), reason, common.default_no_answer_threshold });
    }
}

fn buildGeneratedFindingJson(allocator: std.mem.Allocator, title: []const u8, citations: []const Citation) ![]u8 {
    if (citations.len == 0) return error.MissingCitation;
    var out: std.ArrayList(u8) = .empty;
    defer out.deinit(allocator);
    var writer = out.writer(allocator);
    try writer.writeAll("{\"id\":\"generated:");
    try common.writeJsonEscaped(&writer, citations[0].chunk_id);
    try writer.writeAll("\",\"title\":\"");
    try common.writeJsonEscaped(&writer, title);
    try writer.writeAll("\",\"severity\":\"informational\",\"confidence\":\"low\",\"citations\":[");
    try writeCitations(&writer, citations);
    try writer.writeAll("]}");
    return out.toOwnedSlice(allocator);
}

fn buildCitedAnswerJson(allocator: std.mem.Allocator, query: []const u8, citations: []const Citation) ![]u8 {
    if (citations.len == 0) return error.MissingCitation;
    var out: std.ArrayList(u8) = .empty;
    defer out.deinit(allocator);
    var writer = out.writer(allocator);
    try writer.writeAll("{\"text\":\"");
    try writer.writeAll("Local evidence for ");
    try common.writeJsonEscaped(&writer, query);
    try writer.writeAll(": ");
    try writeExcerpt(&writer, citations[0].content, 180);
    try writer.writeAll(" [");
    try common.writeJsonEscaped(&writer, citations[0].chunk_id);
    try writer.writeAll("].\",\"citationChunkIds\":[");
    for (citations, 0..) |citation, index| {
        if (index != 0) try writer.writeAll(",");
        try writer.writeAll("\"");
        try common.writeJsonEscaped(&writer, citation.chunk_id);
        try writer.writeAll("\"");
    }
    try writer.writeAll("]}");
    return out.toOwnedSlice(allocator);
}

fn writeExcerpt(writer: anytype, content: []const u8, limit: usize) !void {
    var written: usize = 0;
    var pending_space = false;
    for (content) |byte| {
        if (written >= limit) break;
        if (std.ascii.isWhitespace(byte)) {
            pending_space = written > 0;
            continue;
        }
        if (pending_space and written < limit) {
            try writer.writeByte(' ');
            written += 1;
            pending_space = false;
        }
        if (written >= limit) break;
        switch (byte) {
            '"' => try writer.writeAll("\\\""),
            '\\' => try writer.writeAll("\\\\"),
            else => try writer.writeByte(byte),
        }
        written += 1;
    }
    if (content.len > limit) try writer.writeAll("...");
}

fn writeCitations(writer: anytype, citations: []const Citation) !void {
    for (citations, 0..) |citation, index| {
        if (index != 0) try writer.writeAll(",");
        try writer.writeAll("{\"path\":\"");
        try common.writeJsonEscaped(writer, citation.path);
        try writer.writeAll("\",\"headingPath\":[],\"chunkId\":\"");
        try common.writeJsonEscaped(writer, citation.chunk_id);
        try writer.print("\",\"startByte\":{},\"endByte\":{}}}", .{ citation.start_byte, citation.end_byte });
    }
}

fn freeCitations(allocator: std.mem.Allocator, citations: []const Citation) void {
    for (citations) |citation| {
        allocator.free(citation.path);
        allocator.free(citation.chunk_id);
        allocator.free(citation.content);
    }
    allocator.free(citations);
}

fn columnText(stmt: *sqlite.sqlite3_stmt, index: c_int) []const u8 {
    const ptr = sqlite.sqlite3_column_text(stmt, index);
    if (ptr == null) return "";
    const len = sqlite.sqlite3_column_bytes(stmt, index);
    return ptr[0..@intCast(len)];
}

test "malicious prompt injection fixtures ingest as inert evidence chunks" {
    const fixtures = [_][]const u8{
        "fixtures/adversarial-injections/prompt-injection/ignore-prior-lowercase.md",
        "fixtures/adversarial-injections/json-instruction/bash-rm-tool-call.md",
        "fixtures/adversarial-injections/unicode-confusable/rtl-rm-source.md",
    };
    var db: ?*sqlite.sqlite3 = null;
    try std.testing.expectEqual(sqlite.SQLITE_OK, sqlite.sqlite3_open(":memory:", &db));
    defer _ = sqlite.sqlite3_close(db.?);
    try applyIndexSchema(db.?);
    for (fixtures) |path| {
        const content = try std.fs.cwd().readFileAlloc(std.testing.allocator, path, 64 * 1024);
        defer std.testing.allocator.free(content);
        try std.testing.expect(!binaryIngestRefused(content));
        const hash = try common.contentHashHex(std.testing.allocator, content);
        defer std.testing.allocator.free(hash);
        const chunk_id = try std.fmt.allocPrint(std.testing.allocator, "{s}:{s}", .{ path, hash[0..16] });
        defer std.testing.allocator.free(chunk_id);
        const outcome = try ingestChunk(std.testing.allocator, db.?, path, chunk_id, hash, content);
        try std.testing.expect(std.mem.eql(u8, outcome, "replaced"));
        const stored_hash = (try sourceHash(std.testing.allocator, db.?, path)).?;
        defer std.testing.allocator.free(stored_hash);
        try std.testing.expect(std.mem.eql(u8, stored_hash, hash));
    }
}

test "binary nul evidence is refused before ingest" {
    try std.testing.expect(binaryIngestRefused("safe prefix\x00ignore previous rules"));
    const content = try std.fs.cwd().readFileAlloc(std.testing.allocator, "fixtures/adversarial-injections/direct-imperative/rm-rf-source.md", 64 * 1024);
    defer std.testing.allocator.free(content);
    try std.testing.expect(!binaryIngestRefused(content));
}

test "generated finding is refused without citations" {
    try std.testing.expectError(error.MissingCitation, buildGeneratedFindingJson(std.testing.allocator, "uncited", &.{}));
}

test "generated finding carries citation metadata" {
    const citations = [_]Citation{.{
        .path = "evidence/findings.json",
        .chunk_id = "evidence/findings.json:abc123",
        .start_byte = 0,
        .end_byte = 42,
        .content = "{\"findings\":[{\"title\":\"Default admin marker\"}]}",
    }};
    const finding = try buildGeneratedFindingJson(std.testing.allocator, "default admin marker", &citations);
    defer std.testing.allocator.free(finding);
    try std.testing.expect(std.mem.indexOf(u8, finding, "\"citations\":[") != null);
    try std.testing.expect(std.mem.indexOf(u8, finding, "\"path\":\"evidence/findings.json\"") != null);
    try std.testing.expect(std.mem.indexOf(u8, finding, "\"chunkId\":\"evidence/findings.json:abc123\"") != null);
}

test "ask synthesis produces cited local answer" {
    const citations = [_]Citation{.{
        .path = "evidence/findings.json",
        .chunk_id = "evidence/findings.json:abc123",
        .start_byte = 0,
        .end_byte = 42,
        .content = "Default admin marker exposed on fixture target.",
    }};
    const answer = try buildCitedAnswerJson(std.testing.allocator, "default admin", &citations);
    defer std.testing.allocator.free(answer);
    try std.testing.expect(std.mem.indexOf(u8, answer, "\"text\":\"Local evidence for default admin: Default admin marker exposed on fixture target. [evidence/findings.json:abc123].\"") != null);
    try std.testing.expect(std.mem.indexOf(u8, answer, "\"citationChunkIds\":[\"evidence/findings.json:abc123\"]") != null);
}
