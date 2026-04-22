import Foundation
import XCTest
@testable import OnibiSessionProxy

final class TerminalEventParserTests: XCTestCase {
    func testParsesOSC7WorkingDirectory() {
        var parser = TerminalEventParser()

        let events = parser.consume(Data("\u{1B}]7;file://localhost/tmp/onibi%20repo\u{07}".utf8))

        XCTAssertEqual(events, [.workingDirectory("/tmp/onibi repo")])
    }

    func testParsesBellOutsideOSCOnly() {
        var parser = TerminalEventParser()

        let events = parser.consume(Data("build failed\u{07}\u{1B}]0;title\u{07}".utf8))

        XCTAssertEqual(events, [.bell])
    }

    func testParsesSplitOSC7() {
        var parser = TerminalEventParser()

        XCTAssertEqual(parser.consume(Data("\u{1B}]7;file://localhost/Users".utf8)), [])
        XCTAssertEqual(parser.consume(Data("/onibi/project\u{1B}\\".utf8)), [.workingDirectory("/Users/onibi/project")])
    }
}
