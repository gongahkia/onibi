import XCTest
@testable import Onibi
final class LogFileTruncatorTests: XCTestCase {
    func testFormattedSizeDefault() {
        let truncator = LogFileTruncator.shared
        let formatted = truncator.formattedSize
        XCTAssertNotNil(formatted) // should return a valid string like "0 bytes" or similar
    }
    func testSizePercentage() {
        let truncator = LogFileTruncator.shared
        let pct = truncator.sizePercentage(maxMB: 10)
        XCTAssertGreaterThanOrEqual(pct, 0.0)
        XCTAssertLessThanOrEqual(pct, 1.0)
    }
    func testIsStorageLow() {
        let truncator = LogFileTruncator.shared
        let low = truncator.isStorageLow(maxMB: 10)
        XCTAssertNotNil(low) // boolean check, shouldn't crash
    }
    func testTotalLogsDirectorySize() {
        let truncator = LogFileTruncator.shared
        let size = truncator.totalLogsDirectorySize()
        XCTAssertGreaterThanOrEqual(size, 0)
    }
    func testGetRotatedLogFiles() {
        let truncator = LogFileTruncator.shared
        let files = truncator.getRotatedLogFiles()
        XCTAssertNotNil(files) // should return array (possibly empty)
    }
    func testStartStopMonitoring() {
        let truncator = LogFileTruncator.shared
        truncator.startMonitoring(intervalSeconds: 999)
        truncator.stopMonitoring()
        // no crash = pass
    }
}
