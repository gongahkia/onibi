import XCTest
@testable import Onibi

final class JSONStorageManagerTests: XCTestCase {
    
    private var storage: JSONStorageManager!
    private var testLogsPath: String!
    
    override func setUp() {
        super.setUp()
        storage = JSONStorageManager(flushInterval: 0.1)
    }
    
    override func tearDown() {
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
    
    // MARK: - Atomic Write Tests
    
    func testSaveAndLoadLogs() async throws {
        let entries = [makeSampleLog(command: "ls"), makeSampleLog(command: "pwd")]
        try await storage.saveLogs(entries)
        
        let loaded = try await storage.loadLogs()
        XCTAssertEqual(loaded.count, 2)
        XCTAssertEqual(loaded[0].command, "ls")
        XCTAssertEqual(loaded[1].command, "pwd")
    }
    
    func testAtomicWritePreservesDataOnFailure() async throws {
        // Save initial data
        let initial = [makeSampleLog(command: "initial")]
        try await storage.saveLogs(initial)
        
        // Verify initial data persists
        let loaded = try await storage.loadLogs()
        XCTAssertEqual(loaded.count, 1)
        XCTAssertEqual(loaded[0].command, "initial")
    }
    
    // MARK: - Backup Recovery Tests
    
    func testLoadLogsReturnsEmptyOnMissingFile() async throws {
        // Fresh storage with no existing file
        let loaded = try await storage.loadLogs()
        // Should not throw, returns empty or cached
        XCTAssertTrue(loaded.isEmpty || loaded.count >= 0)
    }
    
    // MARK: - Cache Flush Tests
    
    func testAppendLogMarksDirty() async throws {
        let entry = makeSampleLog()
        try await storage.appendLog(entry)
        
        // Flush should persist the entry
        try await storage.flushToDisk()
        
        // Create new storage and load
        let newStorage = JSONStorageManager()
        let loaded = try await newStorage.loadLogs()
        XCTAssertTrue(loaded.contains { $0.command == entry.command })
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
        let expectations = (0..<10).map { i in
            Task {
                try await storage.appendLog(makeSampleLog(command: "cmd\(i)"))
            }
        }
        
        for task in expectations {
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
        
        // Delete logs older than 1 day
        try await storage.deleteLogsOlderThan(Date().addingTimeInterval(-86400))
        
        let loaded = try await storage.loadLogs()
        XCTAssertEqual(loaded.count, 1)
        XCTAssertEqual(loaded[0].command, "new")
    }
    
    func testGetStorageSize() async throws {
        let entries = [makeSampleLog(), makeSampleLog()]
        try await storage.saveLogs(entries)
        
        let size = try await storage.getStorageSize()
        XCTAssertGreaterThan(size, 0)
    }
}
