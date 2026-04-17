import XCTest
@testable import Onibi

final class TokenAgeCalculatorTests: XCTestCase {
    func testAgeReturnsNilWhenIssuedAtMissing() {
        XCTAssertNil(TokenAgeCalculator.ageInDays(from: nil))
    }

    func testAgeComputesWholeDays() {
        let now = Date()
        let fiveDaysAgo = Calendar.current.date(byAdding: .day, value: -5, to: now)!
        XCTAssertEqual(TokenAgeCalculator.ageInDays(from: fiveDaysAgo, now: now), 5)
    }

    func testShouldRemindReturnsFalseWhenUnderThreshold() {
        XCTAssertFalse(TokenAgeCalculator.shouldRemindToRotate(ageDays: 29, threshold: 30))
    }

    func testShouldRemindReturnsTrueAtThreshold() {
        XCTAssertTrue(TokenAgeCalculator.shouldRemindToRotate(ageDays: 30, threshold: 30))
    }

    func testShouldRemindReturnsFalseWhenAgeUnknown() {
        XCTAssertFalse(TokenAgeCalculator.shouldRemindToRotate(ageDays: nil, threshold: 30))
    }
}
