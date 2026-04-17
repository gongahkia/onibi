import XCTest
@testable import Onibi

final class DiagnosticsBundleTests: XCTestCase {
    func testRedactValueShortCircuitsSmallInputs() {
        XCTAssertEqual(DiagnosticsBundleBuilder.redactValue("ab"), "***")
        XCTAssertEqual(DiagnosticsBundleBuilder.redactValue("abcd"), "***")
    }

    func testRedactValuePreservesPrefixAndLength() {
        let redacted = DiagnosticsBundleBuilder.redactValue("abcdefghij")
        XCTAssertEqual(redacted, "abcd…(10)")
    }

    func testSensitiveKeySetContainsExpectedEntries() {
        XCTAssertTrue(DiagnosticsBundleBuilder.sensitiveKeys.contains("pairingToken"))
        XCTAssertTrue(DiagnosticsBundleBuilder.sensitiveKeys.contains("Authorization"))
        XCTAssertTrue(DiagnosticsBundleBuilder.sensitiveKeys.contains("token"))
    }

    func testRedactSettingsStripsNothingSensitiveFromKnownSafeKeys() {
        let settings = AppSettings(mobileAccessPort: 9001)
        let redacted = DiagnosticsBundleBuilder.redactSettings(settings)
        XCTAssertEqual(redacted["mobileAccessPort"], "9001")
        XCTAssertFalse(redacted.keys.contains("pairingToken"), "pairing token must not leak through redactSettings")
    }
}
