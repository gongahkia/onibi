import Foundation
import XCTest
@testable import OnibiSessionProxy

final class TerminalCommandLifecycleParserTests: XCTestCase {
    func testParsesCommandStartOSC() {
        var parser = TerminalCommandLifecycleParser()

        let events = parser.consume(osc("OnibiCommandStart", [
            "command": b64("npm test"),
            "cwd": b64("/tmp/project")
        ]))

        XCTAssertEqual(events, [.start(command: "npm test", workingDirectory: "/tmp/project")])
    }

    func testParsesCommandEndOSCSplitAcrossChunks() {
        var parser = TerminalCommandLifecycleParser()
        let first = Data("\u{1B}]1337;OnibiCommandEnd;exit=2;".utf8)
        let second = Data("cwd=\(b64("/tmp/project"))\u{07}".utf8)

        XCTAssertEqual(parser.consume(first), [])
        XCTAssertEqual(parser.consume(second), [.end(exitCode: 2, workingDirectory: "/tmp/project")])
    }

    func testIgnoresOtherOSCSequences() {
        var parser = TerminalCommandLifecycleParser()

        let events = parser.consume(Data("\u{1B}]0;Terminal Title\u{07}".utf8))

        XCTAssertEqual(events, [])
    }

    private func osc(_ name: String, _ fields: [String: String]) -> Data {
        let encodedFields = fields
            .sorted { $0.key < $1.key }
            .map { "\($0.key)=\($0.value)" }
            .joined(separator: ";")
        return Data("\u{1B}]1337;\(name);\(encodedFields)\u{07}".utf8)
    }

    private func b64(_ value: String) -> String {
        Data(value.utf8).base64EncodedString()
    }
}
