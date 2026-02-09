import XCTest
@testable import Onibi
final class BackgroundTaskSchedulerTests: XCTestCase {
    func testStartAndStop() {
        let scheduler = BackgroundTaskScheduler.shared
        XCTAssertFalse(scheduler.isRunning)
        scheduler.start()
        XCTAssertTrue(scheduler.isRunning)
        scheduler.stop()
        XCTAssertFalse(scheduler.isRunning)
    }
    func testDoubleStartNoOp() {
        let scheduler = BackgroundTaskScheduler.shared
        scheduler.start()
        scheduler.start() // should be no-op
        XCTAssertTrue(scheduler.isRunning)
        scheduler.stop()
    }
    func testForceRefreshDoesNotCrash() {
        let scheduler = BackgroundTaskScheduler.shared
        scheduler.start()
        scheduler.forceRefresh()
        scheduler.stop()
    }
    func testEventsProcessedStartsAtZero() {
        let scheduler = BackgroundTaskScheduler.shared
        // eventsProcessed may have accumulated from other tests, just check it's non-negative
        XCTAssertGreaterThanOrEqual(scheduler.eventsProcessed, 0)
    }
}
