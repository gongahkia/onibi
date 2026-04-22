import Foundation
import XCTest
@testable import OnibiSessionProxy

final class TerminalTitleParserTests: XCTestCase {
    func testParsesBellTerminatedWindowTitle() {
        var parser = TerminalTitleParser()

        let titles = parser.consume(Data("\u{1B}]0;onibi project\u{07}".utf8))

        XCTAssertEqual(titles, ["onibi project"])
    }

    func testParsesSTTerminatedWindowTitleSplitAcrossChunks() {
        var parser = TerminalTitleParser()

        XCTAssertEqual(parser.consume(Data("\u{1B}]2;editing".utf8)), [])
        let titles = parser.consume(Data(" README\u{1B}\\".utf8))

        XCTAssertEqual(titles, ["editing README"])
    }

    func testIgnoresNonTitleOSCCommands() {
        var parser = TerminalTitleParser()

        let titles = parser.consume(Data("\u{1B}]7;file://localhost/tmp\u{07}".utf8))

        XCTAssertEqual(titles, [])
    }

    func testReturnsLatestTitlesInOrder() {
        var parser = TerminalTitleParser()

        let titles = parser.consume(Data("\u{1B}]0;first\u{07}output\u{1B}]2;second\u{07}".utf8))

        XCTAssertEqual(titles, ["first", "second"])
    }
}
