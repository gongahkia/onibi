import XCTest
@testable import GhosttyMenubar

final class LogFileParserTests: XCTestCase {
    
    var parser: LogFileParser!
    
    override func setUp() {
        super.setUp()
        parser = LogFileParser()
    }
    
    override func tearDown() {
        parser = nil
        super.tearDown()
    }
    
    // MARK: - CMD_START Tests
    
    func testParseCmdStart() {
        let line = "2026-02-09T10:30:00+08:00|CMD_START|12345|echo hello"
        let result = parser.parseLine(line)
        
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.type, .commandStart)
        XCTAssertEqual(result?.command, "echo hello")
    }
    
    func testParseCmdStartWithPipes() {
        let line = "2026-02-09T10:30:00+08:00|CMD_START|12345|cat file.txt | grep pattern | wc -l"
        let result = parser.parseLine(line)
        
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.command, "cat file.txt | grep pattern | wc -l")
    }
    
    func testParseCmdStartWithQuotes() {
        let line = "2026-02-09T10:30:00+08:00|CMD_START|12345|echo \"hello world\""
        let result = parser.parseLine(line)
        
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.command, "echo \"hello world\"")
    }
    
    // MARK: - CMD_END Tests
    
    func testParseCmdEndSuccess() {
        let line = "2026-02-09T10:30:01+08:00|CMD_END|12345|0"
        let result = parser.parseLine(line)
        
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.type, .commandEnd)
        XCTAssertEqual(result?.exitCode, 0)
    }
    
    func testParseCmdEndFailure() {
        let line = "2026-02-09T10:30:01+08:00|CMD_END|12345|1"
        let result = parser.parseLine(line)
        
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.type, .commandEnd)
        XCTAssertEqual(result?.exitCode, 1)
    }
    
    func testParseCmdEndNonZero() {
        let line = "2026-02-09T10:30:01+08:00|CMD_END|12345|127"
        let result = parser.parseLine(line)
        
        XCTAssertNotNil(result)
        XCTAssertEqual(result?.exitCode, 127)
    }
    
    // MARK: - Session ID Tests
    
    func testParseWithSessionId() {
        let line = "2026-02-09T10:30:00+08:00|CMD_START|term_session_abc|ls -la"
        let result = parser.parseLine(line)
        
        XCTAssertNotNil(result)
        // Session ID should be captured
    }
    
    func testParseWithPIDAsSession() {
        let line = "2026-02-09T10:30:00+08:00|CMD_START|99999|pwd"
        let result = parser.parseLine(line)
        
        XCTAssertNotNil(result)
    }
    
    // MARK: - Invalid Input Tests
    
    func testParseEmptyLine() {
        let result = parser.parseLine("")
        XCTAssertNil(result)
    }
    
    func testParseMalformedLine() {
        let result = parser.parseLine("not a valid log line")
        XCTAssertNil(result)
    }
    
    func testParsePartialLine() {
        let result = parser.parseLine("2026-02-09T10:30:00+08:00|CMD_START")
        XCTAssertNil(result)
    }
    
    // MARK: - Timestamp Tests
    
    func testParseTimestamp() {
        let line = "2026-02-09T10:30:00+08:00|CMD_START|12345|test"
        let result = parser.parseLine(line)
        
        XCTAssertNotNil(result?.timestamp)
    }
    
    func testParseInvalidTimestamp() {
        let line = "invalid-date|CMD_START|12345|test"
        let result = parser.parseLine(line)
        
        // Should either fail or use current date
        XCTAssertNil(result)
    }
}
