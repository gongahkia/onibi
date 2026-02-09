import XCTest
@testable import Onibi
final class ErrorReporterTests: XCTestCase {
    var reporter: ErrorReporter!
    override func setUp() {
        super.setUp()
        reporter = ErrorReporter.shared
        reporter.clearErrors()
    }
    func testReportAddsError() {
        let exp = expectation(description: "error added")
        reporter.report(title: "Test", message: "test message", severity: .warning)
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) {
            XCTAssertFalse(self.reporter.recentErrors.isEmpty)
            XCTAssertTrue(self.reporter.hasUnreadErrors)
            exp.fulfill()
        }
        wait(for: [exp], timeout: 2.0)
    }
    func testReportErrorInstance() {
        let exp = expectation(description: "error instance added")
        let err = NSError(domain: "test", code: 42, userInfo: [NSLocalizedDescriptionKey: "test error"])
        reporter.report(err, context: "unit test", severity: .info)
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) {
            XCTAssertFalse(self.reporter.recentErrors.isEmpty)
            exp.fulfill()
        }
        wait(for: [exp], timeout: 2.0)
    }
    func testClearErrors() {
        let exp = expectation(description: "errors cleared")
        reporter.report(title: "X", message: "y")
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) {
            self.reporter.clearErrors()
            XCTAssertTrue(self.reporter.recentErrors.isEmpty)
            XCTAssertFalse(self.reporter.hasUnreadErrors)
            exp.fulfill()
        }
        wait(for: [exp], timeout: 2.0)
    }
    func testMarkAsRead() {
        let exp = expectation(description: "marked read")
        reporter.report(title: "A", message: "b")
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.2) {
            self.reporter.markAsRead()
            XCTAssertFalse(self.reporter.hasUnreadErrors)
            exp.fulfill()
        }
        wait(for: [exp], timeout: 2.0)
    }
    func testGenerateGitHubIssueURL() {
        let appError = ErrorReporter.AppError(title: "Crash", message: "null ref")
        let url = reporter.generateGitHubIssueURL(for: appError)
        XCTAssertNotNil(url)
        XCTAssertTrue(url!.absoluteString.contains("github.com"))
        XCTAssertTrue(url!.absoluteString.contains("Crash"))
    }
    func testAppErrorSeverities() {
        let info = ErrorReporter.AppError(title: "T", message: "M", severity: .info)
        XCTAssertEqual(info.severity, .info)
        let critical = ErrorReporter.AppError(title: "T", message: "M", severity: .critical)
        XCTAssertEqual(critical.severity, .critical)
    }
    func testHealthCheck() {
        let issues = reporter.performHealthCheck()
        XCTAssertNotNil(issues) // should run without crash
    }
    func testMaxErrorsCap() {
        let exp = expectation(description: "cap enforced")
        for i in 0..<60 {
            reporter.report(title: "Error \(i)", message: "msg", severity: .info)
        }
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.0) {
            XCTAssertLessThanOrEqual(self.reporter.recentErrors.count, 50)
            exp.fulfill()
        }
        wait(for: [exp], timeout: 3.0)
    }
}
