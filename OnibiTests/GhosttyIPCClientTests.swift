import XCTest
@testable import Onibi
final class GhosttyIPCClientTests: XCTestCase {
    func testCheckGhosttyRunning() {
        let client = GhosttyIPCClient.shared
        let running = client.checkGhosttyRunning()
        XCTAssertNotNil(running) // bool, just verify no crash
    }
    func testGetGhosttyApp() {
        let client = GhosttyIPCClient.shared
        // may return nil if ghostty not running — that's fine
        _ = client.getGhosttyApp()
    }
    func testGetActiveWindows() {
        let client = GhosttyIPCClient.shared
        let sessions = client.getActiveWindows()
        XCTAssertNotNil(sessions)
    }
    func testGhosttyErrorTypes() {
        let notRunning = GhosttyIPCClient.GhosttyError.notRunning
        XCTAssertNotNil(notRunning)
        let notFound = GhosttyIPCClient.GhosttyError.binaryNotFound
        XCTAssertNotNil(notFound)
        let decodeFail = GhosttyIPCClient.GhosttyError.outputDecodingFailed
        XCTAssertNotNil(decodeFail)
    }
    func testGhosttySessionModel() {
        let session = GhosttySession(id: "123", name: "test", pid: 42, isActive: true)
        XCTAssertEqual(session.id, "123")
        XCTAssertEqual(session.name, "test")
        XCTAssertEqual(session.pid, 42)
        XCTAssertTrue(session.isActive)
        XCTAssertEqual(session.commandCount, 0)
    }
    func testExecuteCommandFailsWhenNotRunning() async {
        let client = GhosttyIPCClient.shared
        // ensure not running state for test
        if !client.checkGhosttyRunning() {
            do {
                _ = try await client.executeCommand(["--version"])
                XCTFail("should have thrown")
            } catch {
                // expected: GhosttyError.notRunning
            }
        }
    }
}
