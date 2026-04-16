import XCTest
import OnibiCore
@testable import OnibiSessionProxy

final class RemoteInputByteTranslatorTests: XCTestCase {
    func testTextPayloadUsesUTF8Bytes() throws {
        let payload = RemoteInputPayload.text("npm test")
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: payload), Data("npm test".utf8))
    }

    func testKeyMappingsMatchV1Contract() throws {
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .enter), Data([0x0D]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .ctrlC), Data([0x03]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .ctrlD), Data([0x04]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .tab), Data([0x09]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .backspace), Data([0x7F]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .escape), Data([0x1B]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .delete), Data([0x1B, 0x5B, 0x33, 0x7E]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .home), Data([0x1B, 0x5B, 0x48]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .end), Data([0x1B, 0x5B, 0x46]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .pageUp), Data([0x1B, 0x5B, 0x35, 0x7E]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .pageDown), Data([0x1B, 0x5B, 0x36, 0x7E]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .arrowUp), Data([0x1B, 0x5B, 0x41]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .arrowDown), Data([0x1B, 0x5B, 0x42]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .arrowLeft), Data([0x1B, 0x5B, 0x44]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .arrowRight), Data([0x1B, 0x5B, 0x43]))
        XCTAssertEqual(try RemoteInputByteTranslator.data(for: .space), Data([0x20]))
    }
}
