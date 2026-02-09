import XCTest
@testable import GhosttyMenubar

final class FilterRuleTests: XCTestCase {
    
    // MARK: - Contains Mode Tests
    
    func testContainsMatch() {
        let rule = FilterRule(
            id: UUID(),
            name: "Test",
            pattern: "error",
            matchMode: .contains,
            action: .highlight,
            isEnabled: true
        )
        
        XCTAssertTrue(rule.matches("This contains an error message"))
        XCTAssertTrue(rule.matches("error at line 10"))
        XCTAssertFalse(rule.matches("All tests passed"))
    }
    
    func testContainsCaseInsensitive() {
        let rule = FilterRule(
            id: UUID(),
            name: "Test",
            pattern: "error",
            matchMode: .contains,
            action: .highlight,
            isEnabled: true
        )
        
        XCTAssertTrue(rule.matches("ERROR: Something went wrong"))
        XCTAssertTrue(rule.matches("Error occurred"))
    }
    
    // MARK: - Prefix Mode Tests
    
    func testPrefixMatch() {
        let rule = FilterRule(
            id: UUID(),
            name: "Test",
            pattern: "[ERROR]",
            matchMode: .prefix,
            action: .highlight,
            isEnabled: true
        )
        
        XCTAssertTrue(rule.matches("[ERROR] Something failed"))
        XCTAssertFalse(rule.matches("Something [ERROR] in middle"))
    }
    
    // MARK: - Suffix Mode Tests
    
    func testSuffixMatch() {
        let rule = FilterRule(
            id: UUID(),
            name: "Test",
            pattern: "DONE",
            matchMode: .suffix,
            action: .highlight,
            isEnabled: true
        )
        
        XCTAssertTrue(rule.matches("Build DONE"))
        XCTAssertFalse(rule.matches("DONE building"))
    }
    
    // MARK: - Regex Mode Tests
    
    func testRegexMatch() {
        let rule = FilterRule(
            id: UUID(),
            name: "Test",
            pattern: "\\d+ tests? passed",
            matchMode: .regex,
            action: .highlight,
            isEnabled: true
        )
        
        XCTAssertTrue(rule.matches("1 test passed"))
        XCTAssertTrue(rule.matches("42 tests passed"))
        XCTAssertFalse(rule.matches("No tests passed"))
    }
    
    func testRegexInvalidPattern() {
        let rule = FilterRule(
            id: UUID(),
            name: "Test",
            pattern: "[invalid(regex",
            matchMode: .regex,
            action: .highlight,
            isEnabled: true
        )
        
        // Should not crash, just return false
        XCTAssertFalse(rule.matches("any content"))
    }
    
    // MARK: - Disabled Rule Tests
    
    func testDisabledRuleDoesNotMatch() {
        let rule = FilterRule(
            id: UUID(),
            name: "Test",
            pattern: "match",
            matchMode: .contains,
            action: .highlight,
            isEnabled: false
        )
        
        XCTAssertFalse(rule.matches("This should match but won't"))
    }
    
    // MARK: - Action Tests
    
    func testDifferentActions() {
        let excludeRule = FilterRule(
            id: UUID(),
            name: "Exclude",
            pattern: "debug",
            matchMode: .contains,
            action: .exclude,
            isEnabled: true
        )
        
        let highlightRule = FilterRule(
            id: UUID(),
            name: "Highlight",
            pattern: "important",
            matchMode: .contains,
            action: .highlight,
            isEnabled: true
        )
        
        let notifyRule = FilterRule(
            id: UUID(),
            name: "Notify",
            pattern: "alert",
            matchMode: .contains,
            action: .notify,
            isEnabled: true
        )
        
        XCTAssertEqual(excludeRule.action, .exclude)
        XCTAssertEqual(highlightRule.action, .highlight)
        XCTAssertEqual(notifyRule.action, .notify)
    }
    
    // MARK: - Empty Pattern Tests
    
    func testEmptyPattern() {
        let rule = FilterRule(
            id: UUID(),
            name: "Empty",
            pattern: "",
            matchMode: .contains,
            action: .highlight,
            isEnabled: true
        )
        
        // Empty pattern should match everything or nothing depending on implementation
        // Testing that it doesn't crash
        _ = rule.matches("any content")
    }
}
