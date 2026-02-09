import XCTest
@testable import Onibi

final class JSONStorageManagerTests: XCTestCase {
    private var storage: JSONStorageManager!
    override func setUp() {
        super.setUp()
        storage = JSONStorageManager(flushInterval: 0.1)
    }
    override func tearDown() {
        let logsPath = NSHomeDirectory() + "/.config/onibi/logs.json"
        try? FileManager.default.removeItem(atPath: logsPath)
        storage = nil
        super.tearDown()
    }
    // MARK: - Helper
    private func makeSampleLog(command: String = "echo test") -> LogEntry {
        LogEntry(
            command: command,
            output: "test output",
            exitCode: 0
        )
    }
    // MARK: - Save/Load Tests
    func testSaveAndLoadLogs() async throws {
        let entries = [makeSampleLog(command: "ls"), makeSampleLog(command: "pwd")]
        try await storage.saveLogs(entries)
        let loaded = try await storage.loadLogs()
        XCTAssertEqual(loaded.count, 2)
        XCTAssertEqual(loaded[0].command, "ls")
        XCTAssertEqual(loaded[1].command, "pwd")
    }
    func testAtomicWritePreservesDataOnFailure() async throws {
        let initial = [makeSampleLog(command: "initial")]
        try await storage.saveLogs(initial)
        let loaded = try await storage.loadLogs()
        XCTAssertEqual(loaded.count, 1)
        guard loaded.count == 1 else { return }
        XCTAssertEqual(loaded[0].command, "initial")
    }
    // MARK: - Backup Recovery Tests
    func testLoadLogsReturnsEmptyOnMissingFile() async throws {
        try? await storage.clearAllLogs()
        let loaded = try await storage.loadLogs()
        XCTAssertTrue(loaded.isEmpty)
    }
    // MARK: - Cache Flush Tests
    func testAppendLogAndFlush() async throws {
        let entry = makeSampleLog(command: "append-test")
        try await storage.appendLog(entry)
        try await storage.flushToDisk()
        let loaded = try await storage.loadLogs()
        XCTAssertTrue(loaded.contains { $0.command == "append-test" })
    }
    func testClearAllLogsEmptiesStorage() async throws {
        let entries = [makeSampleLog(), makeSampleLog()]
        try await storage.saveLogs(entries)
        try await storage.clearAllLogs()
        let loaded = try await storage.loadLogs()
        XCTAssertEqual(loaded.count, 0)
    }
    // MARK: - Concurrent Access Tests
    func testConcurrentAppends() async throws {
        let tasks = (0..<10).map { i in
            Task {
                try await storage.appendLog(makeSampleLog(command: "cmd\(i)"))
            }
        }
        for task in tasks {
            try await task.value
        }
        try await storage.flushToDisk()
        let loaded = try await storage.loadLogs()
        XCTAssertGreaterThanOrEqual(loaded.count, 10)
    }
    func testDeleteLogsOlderThanDate() async throws {
        let oldEntry = LogEntry(
            timestamp: Date().addingTimeInterval(-86400 * 7),
            command: "old",
            output: ""
        )
        let newEntry = makeSampleLog(command: "new")
        try await storage.saveLogs([oldEntry, newEntry])
        try await storage.deleteLogsOlderThan(Date().addingTimeInterval(-86400))
        let loaded = try await storage.loadLogs()
        XCTAssertEqual(loaded.count, 1)
        guard loaded.count == 1 else { return }
        XCTAssertEqual(loaded[0].command, "new")
    }
    func testGetStorageSize() async throws {
        let entries = [makeSampleLog(), makeSampleLog()]
        try await storage.saveLogs(entries)
        let size = try await storage.getStorageSize()
        XCTAssertGreaterThan(size, 0)
    }
}
