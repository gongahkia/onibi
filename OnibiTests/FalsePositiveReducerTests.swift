import XCTest
@testable import Onibi

final class FalsePositiveReducerTests: XCTestCase {
    private var reducer: FalsePositiveReducer!
    override func setUp() {
        super.setUp()
        reducer = FalsePositiveReducer.shared
        reducer.suppressedPatterns.removeAll()
        reducer.dismissalCounts.removeAll()
    }
    override func tearDown() {
        reducer.suppressedPatterns.removeAll()
        reducer.dismissalCounts.removeAll()
        super.tearDown()
    }
    // MARK: - Deduplication Window Tests
    func testIsDuplicateReturnsTrueForSameContent() {
        let content = "dedup-same-\(UUID().uuidString)"
        XCTAssertFalse(reducer.isDuplicate(content))
        XCTAssertTrue(reducer.isDuplicate(content))
    }
    func testIsDuplicateReturnsFalseForDifferentContent() {
        let content1 = "dedup-diff1-\(UUID().uuidString)"
        let content2 = "dedup-diff2-\(UUID().uuidString)"
        XCTAssertFalse(reducer.isDuplicate(content1))
        XCTAssertFalse(reducer.isDuplicate(content2))
    }
    func testDeduplicationWindowExpires() async throws {
        let content = "dedup-expire-\(UUID().uuidString)"
        XCTAssertFalse(reducer.isDuplicate(content))
        XCTAssertTrue(reducer.isDuplicate(content))
        try await Task.sleep(nanoseconds: 6_000_000_000) // 6s > 5s window
        XCTAssertFalse(reducer.isDuplicate(content))
    }
    // MARK: - Confidence Scoring Tests
    func testCalculateConfidenceBaseScore() {
        let confidence = reducer.calculateConfidence(
            for: "test content",
            matchedPattern: "test",
            isRegex: false,
            context: nil
        )
        XCTAssertEqual(confidence, 0.5, accuracy: 0.01)
    }
    func testCalculateConfidenceRegexBoost() {
        let confidence = reducer.calculateConfidence(
            for: "test content",
            matchedPattern: "test",
            isRegex: true,
            context: nil
        )
        XCTAssertEqual(confidence, 0.7, accuracy: 0.01)
    }
    func testCalculateConfidenceLongPatternBoost() {
        let confidence = reducer.calculateConfidence(
            for: "test content",
            matchedPattern: "very long pattern here",
            isRegex: false,
            context: nil
        )
        XCTAssertEqual(confidence, 0.6, accuracy: 0.01)
    }
    func testCalculateConfidenceShortContentPenalty() {
        let confidence = reducer.calculateConfidence(
            for: "abc",
            matchedPattern: "test",
            isRegex: false,
            context: nil
        )
        XCTAssertEqual(confidence, 0.3, accuracy: 0.01)
    }
    func testCalculateConfidenceWithContext() {
        let context = FalsePositiveReducer.DetectionContext(
            linesBefore: ["Building project"],
            linesAfter: ["Build completed successfully"],
            matchedPattern: "test"
        )
        let confidence = reducer.calculateConfidence(
            for: "test content",
            matchedPattern: "test",
            isRegex: false,
            context: context
        )
        XCTAssertEqual(confidence, 0.65, accuracy: 0.01)
    }
    // MARK: - Suppression Pattern Tests
    func testMarkAsFalsePositive() {
        let pattern = "benign warning"
        reducer.markAsFalsePositive(pattern)
        XCTAssertTrue(reducer.suppressedPatterns.contains(pattern))
    }
    func testIsSuppressed() {
        let pattern = "ignore this"
        reducer.markAsFalsePositive(pattern)
        XCTAssertTrue(reducer.isSuppressed("Please ignore this message"))
        XCTAssertFalse(reducer.isSuppressed("Different message"))
    }
    func testRemoveSuppression() {
        let pattern = "temporary suppression"
        reducer.markAsFalsePositive(pattern)
        XCTAssertTrue(reducer.suppressedPatterns.contains(pattern))
        reducer.removeSuppression(pattern)
        XCTAssertFalse(reducer.suppressedPatterns.contains(pattern))
    }
    func testDuplicateSuppressionNotAdded() {
        let pattern = "duplicate pattern"
        reducer.markAsFalsePositive(pattern)
        reducer.markAsFalsePositive(pattern)
        XCTAssertEqual(reducer.suppressedPatterns.filter { $0 == pattern }.count, 1)
    }
    // MARK: - Content Length Tests
    func testMeetsMinimumLengthTrue() {
        XCTAssertTrue(reducer.meetsMinimumLength("abc"))
        XCTAssertTrue(reducer.meetsMinimumLength("longer content"))
    }
    func testMeetsMinimumLengthFalse() {
        XCTAssertFalse(reducer.meetsMinimumLength("ab"))
        XCTAssertFalse(reducer.meetsMinimumLength("  "))
        XCTAssertFalse(reducer.meetsMinimumLength(""))
    }
    // MARK: - Dismissal Tracking Tests
    func testRecordDismissal() {
        let type = NotificationType.system
        reducer.recordDismissal(type: type)
        XCTAssertEqual(reducer.dismissalCounts[type], 1)
        reducer.recordDismissal(type: type)
        XCTAssertEqual(reducer.dismissalCounts[type], 2)
    }
    func testSuggestedThrottleIntervalLowDismissals() {
        let type = NotificationType.system
        reducer.dismissalCounts[type] = 3
        let interval = reducer.suggestedThrottleInterval(for: type)
        XCTAssertEqual(interval, 1.0, accuracy: 0.01)
    }
    func testSuggestedThrottleIntervalMediumDismissals() {
        let type = NotificationType.system
        reducer.dismissalCounts[type] = 7
        let interval = reducer.suggestedThrottleInterval(for: type)
        XCTAssertEqual(interval, 2.0, accuracy: 0.01)
    }
    func testSuggestedThrottleIntervalHighDismissals() {
        let type = NotificationType.system
        reducer.dismissalCounts[type] = 15
        let interval = reducer.suggestedThrottleInterval(for: type)
        XCTAssertEqual(interval, 4.0, accuracy: 0.01)
    }
    func testResetDismissalCounts() {
        let type = NotificationType.system
        reducer.dismissalCounts[type] = 10
        reducer.resetDismissalCounts()
        XCTAssertTrue(reducer.dismissalCounts.isEmpty)
    }
    // MARK: - Combined Filter Tests
    func testShouldNotifyWithValidContent() {
        let content = "valid-notify-content-\(UUID().uuidString)" // unique to avoid dedup
        let result = reducer.shouldNotify(
            content: content,
            pattern: "valid",
            type: .system,
            context: nil
        )
        XCTAssertTrue(result.matched)
        XCTAssertGreaterThan(result.confidence, 0)
    }
    func testShouldNotifyWithShortContent() {
        let result = reducer.shouldNotify(
            content: "ab",
            pattern: "ab",
            type: .system,
            context: nil
        )
        XCTAssertFalse(result.matched)
        XCTAssertEqual(result.confidence, 0)
    }
    func testShouldNotifyWithSuppressedContent() {
        reducer.markAsFalsePositive("suppressed")
        let result = reducer.shouldNotify(
            content: "suppressed warning content here",
            pattern: "warning",
            type: .system,
            context: nil
        )
        XCTAssertFalse(result.matched)
    }
    func testShouldNotifyWithDuplicateContent() {
        let content = "duplicate-msg-\(UUID().uuidString)"
        _ = reducer.shouldNotify(content: content, pattern: "message", type: .system, context: nil)
        let result = reducer.shouldNotify(content: content, pattern: "message", type: .system, context: nil)
        XCTAssertFalse(result.matched)
    }
}
