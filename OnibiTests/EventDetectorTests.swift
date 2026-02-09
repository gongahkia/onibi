import XCTest
@testable import Onibi

final class EventDetectorTests: XCTestCase {
    // MARK: - AI Response Detector Tests
    func testAIResponseDetectorPositive() {
        let detector = AIResponseDetector()
        XCTAssertTrue(detector.matches("[Claude] Here is the response"))
        XCTAssertTrue(detector.matches("[GPT] Response text"))
        XCTAssertTrue(detector.matches("[AI] Processing complete"))
        XCTAssertTrue(detector.matches("claude: here is the answer"))
        XCTAssertTrue(detector.matches("assistant: I can help"))
    }
    func testAIResponseDetectorNegative() {
        let detector = AIResponseDetector()
        XCTAssertFalse(detector.matches("npm install complete"))
        XCTAssertFalse(detector.matches("git push origin main"))
        XCTAssertFalse(detector.matches("ls -la"))
        XCTAssertFalse(detector.matches("echo hello"))
    }
    // MARK: - Task Completion Detector Tests
    func testTaskCompletionDetectorPositive() {
        let detector = TaskCompletionDetector()
        XCTAssertTrue(detector.matches("✓ All good"))
        XCTAssertTrue(detector.matches("✔ Done"))
        XCTAssertTrue(detector.matches("[DONE] finished"))
        XCTAssertTrue(detector.matches("Task completed successfully"))
        XCTAssertTrue(detector.matches("Successfully installed"))
    }
    func testTaskCompletionDetectorNegative() {
        let detector = TaskCompletionDetector()
        XCTAssertFalse(detector.matches("Starting build..."))
        XCTAssertFalse(detector.matches("Running tests..."))
        XCTAssertFalse(detector.matches("Installing dependencies..."))
    }
    // MARK: - Dev Workflow Detector Tests
    func testDevWorkflowDetectorPositive() {
        let parser = DevWorkflowParser()
        XCTAssertTrue(parser.matches("Build succeeded"))
        XCTAssertTrue(parser.matches("Compilation finished"))
        XCTAssertTrue(parser.matches("swift build"))
        XCTAssertTrue(parser.matches("npm test"))
        XCTAssertTrue(parser.matches("cargo test"))
    }
    func testDevWorkflowDetectorNegative() {
        let parser = DevWorkflowParser()
        XCTAssertFalse(parser.matches("Hello world"))
        XCTAssertFalse(parser.matches("cd /home/user"))
    }
    // MARK: - Case Sensitivity Tests
    func testDetectorsCaseInsensitive() {
        let aiDetector = AIResponseDetector()
        let devParser = DevWorkflowParser()
        XCTAssertTrue(aiDetector.matches("[CLAUDE] response"))
        XCTAssertTrue(aiDetector.matches("[claude] response"))
        XCTAssertTrue(devParser.matches("BUILD SUCCEEDED"))
        XCTAssertTrue(devParser.matches("build succeeded"))
    }
    // MARK: - Empty Input Tests
    func testDetectorsEmptyInput() {
        let aiDetector = AIResponseDetector()
        let taskDetector = TaskCompletionDetector()
        let devParser = DevWorkflowParser()
        XCTAssertFalse(aiDetector.matches(""))
        XCTAssertFalse(taskDetector.matches(""))
        XCTAssertFalse(devParser.matches(""))
    }
    // MARK: - Unicode Tests
    func testDetectorsWithUnicode() {
        let taskDetector = TaskCompletionDetector()
        XCTAssertTrue(taskDetector.matches("✓ Build complete"))
        XCTAssertTrue(taskDetector.matches("✔ Tests passed"))
    }
}
