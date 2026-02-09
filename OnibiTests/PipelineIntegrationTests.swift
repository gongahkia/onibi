import XCTest
@testable import Onibi

final class PipelineIntegrationTests: XCTestCase {
    private var tempLogPath: String!
    private var tempDir: String!
    private var fileWatcher: FileWatcher?
    private var logBuffer: LogBuffer!
    private var eventDetected: Bool = false
    private var detectedEvents: [GhosttyEvent] = []
    override func setUp() {
        super.setUp()
        tempDir = NSTemporaryDirectory() + "onibi-integration-\(UUID().uuidString)"
        try? FileManager.default.createDirectory(atPath: tempDir, withIntermediateDirectories: true)
        tempLogPath = tempDir + "/test.log"
        eventDetected = false
        detectedEvents = []
        try? "".write(toFile: tempLogPath, atomically: true, encoding: .utf8)
        logBuffer = LogBuffer(filePath: tempLogPath)
    }
    override func tearDown() {
        fileWatcher?.stop()
        fileWatcher = nil
        try? FileManager.default.removeItem(atPath: tempDir)
        super.tearDown()
    }
    // MARK: - Helper Methods
    private func writeLogLine(_ line: String) throws {
        let fileHandle = try FileHandle(forWritingTo: URL(fileURLWithPath: tempLogPath))
        try fileHandle.seekToEnd()
        if let data = (line + "\n").data(using: .utf8) {
            try fileHandle.write(contentsOf: data)
        }
        try fileHandle.close()
    }
    private func createTimestampedLogLine(type: String, sessionId: String, content: String) -> String {
        let timestamp = ISO8601DateFormatter().string(from: Date())
        return "\(timestamp)|\(type)|\(sessionId)|\(content)"
    }
    // MARK: - Pipeline Tests
    func testPipelineWithFileRotation() async throws {
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "session1",
            content: "Initial content"
        ))
        let content1 = try logBuffer.readNewContent()
        XCTAssertTrue(content1.contains("Initial content"))
        try FileManager.default.removeItem(atPath: tempLogPath)
        try "".write(toFile: tempLogPath, atomically: true, encoding: .utf8)
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "session2",
            content: "After rotation"
        ))
        let content2 = try logBuffer.readNewContent()
        XCTAssertTrue(content2.contains("After rotation"))
        XCTAssertFalse(content2.contains("Initial content"))
    }
    func testPipelineWithIncrementalReading() throws {
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_START",
            sessionId: "session1",
            content: "git status"
        ))
        let content1 = try logBuffer.readNewContent()
        XCTAssertTrue(content1.contains("git status"))
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "session1",
            content: "On branch main"
        ))
        let content2 = try logBuffer.readNewContent()
        XCTAssertTrue(content2.contains("On branch main"))
        XCTAssertFalse(content2.contains("git status"))
        let content3 = try logBuffer.readNewContent()
        XCTAssertTrue(content3.isEmpty)
    }
    func testPipelineWithEventDetectorChain() throws {
        let detectors: [EventParser] = [
            AIResponseDetector(),
            TaskCompletionDetector(),
            DevWorkflowParser()
        ]
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "test",
            content: "Build succeeded in 42.3s"
        ))
        let newContent = try logBuffer.readNewContent()
        let lines = newContent.components(separatedBy: .newlines).filter { !$0.isEmpty }
        var matchedDetector: EventParser?
        for line in lines {
            let parts = line.components(separatedBy: "|")
            guard parts.count >= 4 else { continue }
            let content = parts[3]
            for detector in detectors {
                if detector.matches(content) {
                    matchedDetector = detector
                    break
                }
            }
        }
        // DevWorkflowParser should match "Build succeeded"
        XCTAssertNotNil(matchedDetector)
        XCTAssertTrue(matchedDetector is DevWorkflowParser)
    }
    func testPipelineNotificationEmission() async throws {
        let expectation = XCTestExpectation(description: "Notification emitted")
        var notificationReceived = false
        let subscription = EventBus.shared.eventPublisher
            .sink { event in
                if event.type == .error {
                    notificationReceived = true
                    expectation.fulfill()
                }
            }
        defer { subscription.cancel() }
        let event = GhosttyEvent(
            timestamp: Date(),
            type: .error,
            command: nil,
            output: "Error occurred"
        )
        EventBus.shared.publish(event)
        await fulfillment(of: [expectation], timeout: 1.0)
        XCTAssertTrue(notificationReceived)
    }
    func testPipelineWithFalsePositiveFiltering() async throws {
        let reducer = FalsePositiveReducer.shared
        reducer.suppressedPatterns.removeAll()
        reducer.markAsFalsePositive("benign error")
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "test",
            content: "This is a benign error that should be ignored"
        ))
        let newContent = try logBuffer.readNewContent()
        let parts = newContent.components(separatedBy: "|")
        guard parts.count >= 4 else {
            XCTFail("Invalid log format")
            return
        }
        let content = parts[3]
        XCTAssertTrue(reducer.isSuppressed(content))
        let result = reducer.shouldNotify(content: content, pattern: "error", type: .system, context: nil)
        XCTAssertFalse(result.matched)
    }
}
