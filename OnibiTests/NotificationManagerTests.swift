import XCTest
@testable import Onibi
final class NotificationManagerTests: XCTestCase {
    func testCategoryFromNotificationType() {
        XCTAssertEqual(NotificationManager.Category.from(.system), .system)
        XCTAssertEqual(NotificationManager.Category.from(.taskCompletion), .taskCompletion)
        XCTAssertEqual(NotificationManager.Category.from(.aiOutput), .aiOutput)
        XCTAssertEqual(NotificationManager.Category.from(.devWorkflow), .devWorkflow)
        XCTAssertEqual(NotificationManager.Category.from(.automation), .automation)
        XCTAssertEqual(NotificationManager.Category.from(.terminalNotification), .system)
    }
    func testCategoryRawValues() {
        XCTAssertEqual(NotificationManager.Category.system.rawValue, "GHOSTTY_SYSTEM")
        XCTAssertEqual(NotificationManager.Category.taskCompletion.rawValue, "GHOSTTY_TASK")
        XCTAssertEqual(NotificationManager.Category.aiOutput.rawValue, "GHOSTTY_AI")
        XCTAssertEqual(NotificationManager.Category.devWorkflow.rawValue, "GHOSTTY_DEV")
        XCTAssertEqual(NotificationManager.Category.automation.rawValue, "GHOSTTY_AUTOMATION")
    }
    func testActionRawValues() {
        XCTAssertEqual(NotificationManager.Action.viewInApp.rawValue, "VIEW_IN_APP")
        XCTAssertEqual(NotificationManager.Action.dismiss.rawValue, "DISMISS")
        XCTAssertEqual(NotificationManager.Action.openTerminal.rawValue, "OPEN_TERMINAL")
    }
    func testCategoryCaseIterable() {
        let allCases = NotificationManager.Category.allCases
        XCTAssertEqual(allCases.count, 5)
    }
}
