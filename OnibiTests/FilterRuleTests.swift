import XCTest
@testable import Onibi

final class FilterRuleTests: XCTestCase {
    // MARK: - Init Tests
    func testDefaultInit() {
        let rule = FilterRule(name: "Test", pattern: "error")
        XCTAssertEqual(rule.name, "Test")
        XCTAssertEqual(rule.pattern, "error")
        XCTAssertTrue(rule.isEnabled)
        XCTAssertFalse(rule.isRegex)
        XCTAssertEqual(rule.matchType, .contains)
        XCTAssertEqual(rule.action, .highlight)
    }
    func testCustomInit() {
        let rule = FilterRule(
            name: "Custom",
            isEnabled: false,
            pattern: "warn.*",
            isRegex: true,
            matchType: .regex,
            action: .notify
        )
        XCTAssertEqual(rule.name, "Custom")
        XCTAssertFalse(rule.isEnabled)
        XCTAssertEqual(rule.pattern, "warn.*")
        XCTAssertTrue(rule.isRegex)
        XCTAssertEqual(rule.matchType, .regex)
        XCTAssertEqual(rule.action, .notify)
    }
    // MARK: - MatchType Tests
    func testMatchTypeCases() {
        let cases = MatchType.allCases
        XCTAssertTrue(cases.contains(.contains))
        XCTAssertTrue(cases.contains(.startsWith))
        XCTAssertTrue(cases.contains(.endsWith))
        XCTAssertTrue(cases.contains(.exact))
        XCTAssertTrue(cases.contains(.regex))
    }
    // MARK: - FilterAction Tests
    func testFilterActionCases() {
        let cases = FilterAction.allCases
        XCTAssertTrue(cases.contains(.highlight))
        XCTAssertTrue(cases.contains(.hide))
        XCTAssertTrue(cases.contains(.notify))
        XCTAssertTrue(cases.contains(.tag))
    }
    // MARK: - Codable Tests
    func testFilterRuleCodable() throws {
        let rule = FilterRule(name: "Codable", pattern: "test", matchType: .startsWith, action: .hide)
        let data = try JSONEncoder().encode(rule)
        let decoded = try JSONDecoder().decode(FilterRule.self, from: data)
        XCTAssertEqual(rule, decoded)
    }
    // MARK: - Equatable Tests
    func testFilterRuleEquality() {
        let id = UUID()
        let rule1 = FilterRule(id: id, name: "A", pattern: "p")
        let rule2 = FilterRule(id: id, name: "A", pattern: "p")
        XCTAssertEqual(rule1, rule2)
    }
    func testFilterRuleInequality() {
        let rule1 = FilterRule(name: "A", pattern: "p")
        let rule2 = FilterRule(name: "B", pattern: "q")
        XCTAssertNotEqual(rule1, rule2)
    }
}
