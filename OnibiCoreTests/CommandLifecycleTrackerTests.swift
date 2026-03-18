import XCTest
@testable import OnibiCore

final class CommandLifecycleTrackerTests: XCTestCase {
    func testCorrelatesStartAndEndIntoCompletedRecord() async {
        let tracker = CommandLifecycleTracker()
        let startTime = Date()
        await tracker.recordStart(
            sessionId: "session-1",
            command: "claude --print hello",
            timestamp: startTime,
            assistantKind: .claudeCode
        )

        let completed = await tracker.complete(
            sessionId: "session-1",
            exitCode: 0,
            endedAt: startTime.addingTimeInterval(1.25)
        )

        XCTAssertNotNil(completed)
        XCTAssertEqual(completed?.sessionId, "session-1")
        XCTAssertEqual(completed?.assistantKind, .claudeCode)
        XCTAssertEqual(completed?.exitCode, 0)
        XCTAssertEqual(completed?.displayCommand, "claude --print +1")
        XCTAssertEqual(try XCTUnwrap(completed?.duration), 1.25, accuracy: 0.001)
    }

    func testCompleteReturnsNilWithoutMatchingStart() async {
        let tracker = CommandLifecycleTracker()
        let completed = await tracker.complete(sessionId: "missing", exitCode: 1, endedAt: Date())
        XCTAssertNil(completed)
    }
}
