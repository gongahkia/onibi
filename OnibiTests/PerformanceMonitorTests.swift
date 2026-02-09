import XCTest
@testable import Onibi
final class PerformanceMonitorTests: XCTestCase {
    func testRecordAndAverage() {
        let monitor = PerformanceMonitor.shared
        monitor.record("test_metric", duration: 0.01)
        monitor.record("test_metric", duration: 0.03)
        let avg = monitor.averageDuration("test_metric")
        XCTAssertNotNil(avg)
        XCTAssertEqual(avg!, 0.02, accuracy: 0.001)
    }
    func testAverageNonExistent() {
        let monitor = PerformanceMonitor.shared
        let avg = monitor.averageDuration("nonexistent_\(UUID().uuidString)")
        XCTAssertNil(avg)
    }
    func testMeasure() {
        let monitor = PerformanceMonitor.shared
        let key = "measure_test_\(UUID().uuidString)"
        monitor.measure(key) {
            _ = (0..<1000).reduce(0, +)
        }
        let avg = monitor.averageDuration(key)
        XCTAssertNotNil(avg)
        XCTAssertGreaterThan(avg!, 0)
    }
    func testReport() {
        let monitor = PerformanceMonitor.shared
        monitor.record("report_test", duration: 0.005)
        let report = monitor.report()
        XCTAssertTrue(report.contains("Performance Report"))
    }
}
