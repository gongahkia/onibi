import XCTest
@testable import Onibi

final class GatewayRequestJournalTests: XCTestCase {
    func testRecordInsertsNewestFirst() {
        let journal = GatewayRequestJournal(capacity: 10)
        journal.record(method: "GET", path: "/a", statusCode: 200, latencyMs: 5, peer: "127.0.0.1")
        journal.record(method: "POST", path: "/b", statusCode: 404, latencyMs: 9, peer: "192.168.1.20")

        let snapshot = journal.snapshot()
        XCTAssertEqual(snapshot.count, 2)
        XCTAssertEqual(snapshot[0].path, "/b")
        XCTAssertEqual(snapshot[1].path, "/a")
    }

    func testCapacityEviction() {
        let journal = GatewayRequestJournal(capacity: 3)
        for i in 0..<5 {
            journal.record(method: "GET", path: "/\(i)", statusCode: 200, latencyMs: 1, peer: "x")
        }
        let snapshot = journal.snapshot()
        XCTAssertEqual(snapshot.count, 3)
        XCTAssertEqual(snapshot.map(\.path), ["/4", "/3", "/2"])
    }

    func testIsSuccessClassification() {
        let ok = GatewayRequestEntry(timestamp: Date(), method: "GET", path: "/", statusCode: 204, latencyMs: 1, peer: "x")
        let bad = GatewayRequestEntry(timestamp: Date(), method: "GET", path: "/", statusCode: 500, latencyMs: 1, peer: "x")
        XCTAssertTrue(ok.isSuccess)
        XCTAssertFalse(bad.isSuccess)
    }
}
