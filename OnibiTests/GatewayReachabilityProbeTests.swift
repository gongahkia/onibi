import XCTest
import Network
@testable import Onibi

final class GatewayReachabilityProbeTests: XCTestCase {
    func testOutcomeLabels() {
        XCTAssertEqual(GatewayProbeOutcome.ok(latencyMs: 42).label, "reachable (42 ms)")
        XCTAssertEqual(GatewayProbeOutcome.badStatus(500).label, "HTTP 500")
        XCTAssertEqual(GatewayProbeOutcome.refused.label, "connection refused")
        XCTAssertEqual(GatewayProbeOutcome.timeout.label, "timed out")
        XCTAssertTrue(GatewayProbeOutcome.ok(latencyMs: 0).isOK)
        XCTAssertFalse(GatewayProbeOutcome.badStatus(500).isOK)
    }

    func testInvalidURLShortCircuits() async {
        let probe = GatewayReachabilityProbe(timeout: 1.0)
        let results = await probe.probeAll(baseURLs: ["not a url"])
        XCTAssertEqual(results.count, 1)
        if case .invalidURL = results[0].outcome {
            // ok
        } else {
            XCTFail("expected invalidURL, got \(results[0].outcome.label)")
        }
    }

    func testRefusedWhenNothingListening() async {
        // 127.0.0.1 on a presumably-free high port should refuse quickly.
        let probe = GatewayReachabilityProbe(timeout: 1.5)
        let results = await probe.probeAll(baseURLs: ["http://127.0.0.1:1"])
        XCTAssertEqual(results.count, 1)
        // Either refused or some other connection failure — what matters is it's not OK.
        XCTAssertFalse(results[0].outcome.isOK)
    }
}
