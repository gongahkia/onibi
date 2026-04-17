import XCTest
@testable import OnibiCore

final class AuthFailureTrackerTests: XCTestCase {
    func testBelowThresholdDoesNotBlock() async {
        let tracker = AuthFailureTracker(windowSeconds: 60, maxFailures: 5)
        for _ in 0..<4 {
            await tracker.recordFailure(peer: "1.2.3.4")
        }
        let decision = await tracker.evaluate(peer: "1.2.3.4")
        XCTAssertFalse(decision.shouldBlock)
    }

    func testAtThresholdBlocks() async {
        let tracker = AuthFailureTracker(windowSeconds: 60, maxFailures: 3)
        for _ in 0..<3 {
            await tracker.recordFailure(peer: "1.2.3.4")
        }
        let decision = await tracker.evaluate(peer: "1.2.3.4")
        XCTAssertTrue(decision.shouldBlock)
        XCTAssertGreaterThan(decision.retryAfterSeconds, 0)
    }

    func testSuccessClearsBucket() async {
        let tracker = AuthFailureTracker(windowSeconds: 60, maxFailures: 3)
        for _ in 0..<3 {
            await tracker.recordFailure(peer: "1.2.3.4")
        }
        await tracker.recordSuccess(peer: "1.2.3.4")
        let decision = await tracker.evaluate(peer: "1.2.3.4")
        XCTAssertFalse(decision.shouldBlock)
    }

    func testFailuresOutsideWindowArePruned() async {
        let tracker = AuthFailureTracker(windowSeconds: 60, maxFailures: 3)
        let oldTimestamp = Date().addingTimeInterval(-120)
        for _ in 0..<3 {
            await tracker.recordFailure(peer: "1.2.3.4", now: oldTimestamp)
        }
        let count = await tracker.failureCount(peer: "1.2.3.4")
        XCTAssertEqual(count, 0, "expired failures should be pruned")
    }

    func testIsolationAcrossPeers() async {
        let tracker = AuthFailureTracker(windowSeconds: 60, maxFailures: 3)
        for _ in 0..<3 {
            await tracker.recordFailure(peer: "1.1.1.1")
        }
        let badDecision = await tracker.evaluate(peer: "1.1.1.1")
        let goodDecision = await tracker.evaluate(peer: "2.2.2.2")
        XCTAssertTrue(badDecision.shouldBlock)
        XCTAssertFalse(goodDecision.shouldBlock)
    }
}
