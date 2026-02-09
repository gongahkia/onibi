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
        
        // Create temp directory for test log files
        tempDir = NSTemporaryDirectory() + "onibi-integration-\(UUID().uuidString)"
        try? FileManager.default.createDirectory(atPath: tempDir, withIntermediateDirectories: true)
        
        tempLogPath = tempDir + "/test.log"
        eventDetected = false
        detectedEvents = []
        
        // Create initial log file
        try? "".write(toFile: tempLogPath, atomically: true, encoding: .utf8)
        
        // Initialize log buffer
        logBuffer = LogBuffer(filePath: tempLogPath)
    }
    
    override func tearDown() {
        fileWatcher?.stop()
        fileWatcher = nil
        
        // Clean up temp directory
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
    
    // MARK: - Full Pipeline Integration Tests
    
    func testFullPipelineWithErrorDetection() async throws {
        let expectation = XCTestExpectation(description: "Error event detected")
        var detectedEvent: GhosttyEvent?
        
        // Set up FileWatcher
        fileWatcher = FileWatcher(path: tempLogPath, debounceInterval: 0.1) { [weak self] in
            guard let self = self else { return }
            
            // Read new content via LogBuffer
            if let newContent = try? self.logBuffer.readNewContent() {
                let lines = newContent.components(separatedBy: .newlines).filter { !$0.isEmpty }
                
                for line in lines {
                    // Parse log line
                    let parts = line.components(separatedBy: "|")
                    guard parts.count >= 4 else { continue }
                    
                    let content = parts[3]
                    
                    // Detect error in content
                    let errorDetector = ErrorDetector()
                    if errorDetector.matches(content) {
                        detectedEvent = GhosttyEvent(
                            timestamp: Date(),
                            type: .error,
                            command: nil,
                            output: content
                        )
                        expectation.fulfill()
                    }
                }
            }
        }
        
        fileWatcher?.start()
        
        // Write log line with error
        let errorLine = createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "test-session",
            content: "Error: File not found"
        )
        try writeLogLine(errorLine)
        
        // Wait for detection
        await fulfillment(of: [expectation], timeout: 2.0)
        
        XCTAssertNotNil(detectedEvent)
        XCTAssertEqual(detectedEvent?.type, .error)
        XCTAssertTrue(detectedEvent?.output?.contains("Error") ?? false)
    }
    
    func testFullPipelineWithBuildSuccess() async throws {
        let expectation = XCTestExpectation(description: "Build success detected")
        var detectedEvent: GhosttyEvent?
        
        fileWatcher = FileWatcher(path: tempLogPath, debounceInterval: 0.1) { [weak self] in
            guard let self = self else { return }
            
            if let newContent = try? self.logBuffer.readNewContent() {
                let lines = newContent.components(separatedBy: .newlines).filter { !$0.isEmpty }
                
                for line in lines {
                    let parts = line.components(separatedBy: "|")
                    guard parts.count >= 4 else { continue }
                    
                    let content = parts[3]
                    
                    // Detect build success
                    let buildDetector = BuildCompletionDetector()
                    if buildDetector.matches(content) {
                        detectedEvent = GhosttyEvent(
                            timestamp: Date(),
                            type: .buildComplete,
                            command: nil,
                            output: content
                        )
                        expectation.fulfill()
                    }
                }
            }
        }
        
        fileWatcher?.start()
        
        // Write log line with build success
        let buildLine = createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "build-session",
            content: "Build succeeded in 42.3s"
        )
        try writeLogLine(buildLine)
        
        await fulfillment(of: [expectation], timeout: 2.0)
        
        XCTAssertNotNil(detectedEvent)
        XCTAssertTrue(detectedEvent?.output?.contains("succeeded") ?? false)
    }
    
    func testFullPipelineWithMultipleEvents() async throws {
        let expectation = XCTestExpectation(description: "Multiple events detected")
        expectation.expectedFulfillmentCount = 2
        var detectedEventTypes: [EventType] = []
        
        fileWatcher = FileWatcher(path: tempLogPath, debounceInterval: 0.1) { [weak self] in
            guard let self = self else { return }
            
            if let newContent = try? self.logBuffer.readNewContent() {
                let lines = newContent.components(separatedBy: .newlines).filter { !$0.isEmpty }
                
                for line in lines {
                    let parts = line.components(separatedBy: "|")
                    guard parts.count >= 4 else { continue }
                    
                    let content = parts[3]
                    
                    // Check multiple detectors
                    let errorDetector = ErrorDetector()
                    if errorDetector.matches(content) {
                        detectedEventTypes.append(.error)
                        expectation.fulfill()
                        continue
                    }
                    
                    let warningDetector = WarningDetector()
                    if warningDetector.matches(content) {
                        detectedEventTypes.append(.warning)
                        expectation.fulfill()
                        continue
                    }
                }
            }
        }
        
        fileWatcher?.start()
        
        // Write multiple log lines
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "session1",
            content: "Error: Connection timeout"
        ))
        
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "session2",
            content: "Warning: Deprecated API usage"
        ))
        
        await fulfillment(of: [expectation], timeout: 2.0)
        
        XCTAssertEqual(detectedEventTypes.count, 2)
        XCTAssertTrue(detectedEventTypes.contains(.error))
        XCTAssertTrue(detectedEventTypes.contains(.warning))
    }
    
    func testPipelineWithFileRotation() async throws {
        // Write initial content
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "session1",
            content: "Initial content"
        ))
        
        // Read it
        let content1 = try logBuffer.readNewContent()
        XCTAssertTrue(content1.contains("Initial content"))
        
        // Simulate file rotation by creating a new file
        try FileManager.default.removeItem(atPath: tempLogPath)
        try "".write(toFile: tempLogPath, atomically: true, encoding: .utf8)
        
        // Write new content to rotated file
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "session2",
            content: "After rotation"
        ))
        
        // Read again - should detect rotation and read new content
        let content2 = try logBuffer.readNewContent()
        XCTAssertTrue(content2.contains("After rotation"))
        XCTAssertFalse(content2.contains("Initial content"))
    }
    
    func testPipelineWithIncrementalReading() throws {
        // Write first line
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_START",
            sessionId: "session1",
            content: "git status"
        ))
        
        // Read first line
        let content1 = try logBuffer.readNewContent()
        XCTAssertTrue(content1.contains("git status"))
        
        // Write second line
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "session1",
            content: "On branch main"
        ))
        
        // Read second line - should only get new content
        let content2 = try logBuffer.readNewContent()
        XCTAssertTrue(content2.contains("On branch main"))
        XCTAssertFalse(content2.contains("git status"))
        
        // Read again with no new content
        let content3 = try logBuffer.readNewContent()
        XCTAssertTrue(content3.isEmpty)
    }
    
    func testPipelineWithEventDetectorChain() throws {
        let detectors: [EventParser] = [
            ErrorDetector(),
            WarningDetector(),
            BuildCompletionDetector(),
            TestCompletionDetector()
        ]
        
        // Write log with error
        try writeLogLine(createTimestampedLogLine(
            type: "CMD_END",
            sessionId: "test",
            content: "Error: Compilation failed"
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
        
        XCTAssertNotNil(matchedDetector)
        XCTAssertTrue(matchedDetector is ErrorDetector)
    }
    
    func testPipelineNotificationEmission() async throws {
        let expectation = XCTestExpectation(description: "Notification emitted")
        var notificationReceived = false
        
        // Subscribe to EventBus
        let subscription = EventBus.shared.eventPublisher
            .sink { event in
                if event.type == .error {
                    notificationReceived = true
                    expectation.fulfill()
                }
            }
        
        defer { subscription.cancel() }
        
        // Emit event through EventBus
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
        
        // Mark a pattern as false positive
        reducer.markAsFalsePositive("benign error")
        
        // Write log with suppressed pattern
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
        
        // Check if it should be suppressed
        XCTAssertTrue(reducer.isSuppressed(content))
        
        // Verify detection result
        let result = reducer.shouldNotify(content: content, pattern: "error", type: .error, context: nil)
        XCTAssertFalse(result.matched)
    }
}
