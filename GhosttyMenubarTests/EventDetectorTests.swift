import XCTest
@testable import GhosttyMenubar

final class EventDetectorTests: XCTestCase {
    
    // MARK: - AI Response Detector Tests
    
    func testAIResponseDetectorPositive() {
        let detector = AIResponseDetector()
        
        XCTAssertTrue(detector.matches("Here's the result from Claude:"))
        XCTAssertTrue(detector.matches("ChatGPT says: Hello"))
        XCTAssertTrue(detector.matches("GPT-4 response completed"))
        XCTAssertTrue(detector.matches("[AI] Processing complete"))
        XCTAssertTrue(detector.matches("Copilot suggestion:"))
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
        
        XCTAssertTrue(detector.matches("✓ Task completed successfully"))
        XCTAssertTrue(detector.matches("Build succeeded"))
        XCTAssertTrue(detector.matches("All tests passed"))
        XCTAssertTrue(detector.matches("Done in 3.2s"))
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
        
        XCTAssertTrue(parser.matches("Compiling src/main.rs"))
        XCTAssertTrue(parser.matches("Building for production..."))
        XCTAssertTrue(parser.matches("Running tests..."))
        XCTAssertTrue(parser.matches("Deploying to production"))
    }
    
    func testDevWorkflowDetectorNegative() {
        let parser = DevWorkflowParser()
        
        XCTAssertFalse(parser.matches("Hello world"))
        XCTAssertFalse(parser.matches("cd /home/user"))
    }
    
    // MARK: - Case Sensitivity Tests
    
    func testDetectorsCaseInsensitive() {
        let aiDetector = AIResponseDetector()
        let taskDetector = TaskCompletionDetector()
        
        XCTAssertTrue(aiDetector.matches("CHATGPT says:"))
        XCTAssertTrue(aiDetector.matches("chatgpt says:"))
        XCTAssertTrue(taskDetector.matches("BUILD SUCCEEDED"))
        XCTAssertTrue(taskDetector.matches("build succeeded"))
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
        
        XCTAssertTrue(taskDetector.matches("✅ Build complete"))
        XCTAssertTrue(taskDetector.matches("🎉 Tests passed"))
    }
}
