import XCTest
@testable import OnibiCore

final class AssistantClassifierTests: XCTestCase {
    func testClassifyClaudeCodeCommand() {
        XCTAssertEqual(AssistantClassifier.classify(command: "claude --print"), .claudeCode)
    }

    func testClassifyCodexCommand() {
        XCTAssertEqual(AssistantClassifier.classify(command: "codex run"), .codex)
    }

    func testClassifyGeminiCommand() {
        XCTAssertEqual(AssistantClassifier.classify(command: "gemini ask"), .gemini)
    }

    func testClassifyCopilotCommand() {
        XCTAssertEqual(AssistantClassifier.classify(command: "github-copilot chat"), .copilot)
    }

    func testClassifyOtherAICommand() {
        XCTAssertEqual(AssistantClassifier.classify(command: "ollama run llama3"), .otherAI)
    }

    func testClassifyUnknownCommand() {
        XCTAssertEqual(AssistantClassifier.classify(command: "swift test"), .unknown)
    }

    func testMetadataOverridesCommand() {
        XCTAssertEqual(
            AssistantClassifier.classify(command: "swift test", metadata: ["assistantKind": "codex"]),
            .codex
        )
    }
}
