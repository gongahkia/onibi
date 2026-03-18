import XCTest
import OnibiCore

final class OnibiPhoneSmokeTests: XCTestCase {
    func testConnectionConfigurationRetainsValues() throws {
        let baseURL = try XCTUnwrap(URL(string: "https://example.tailnet.ts.net"))
        let configuration = MobileConnectionConfiguration(baseURLString: baseURL.absoluteString)

        XCTAssertEqual(configuration.baseURL, baseURL)
        XCTAssertEqual(configuration.baseURLString, baseURL.absoluteString)
    }
}
