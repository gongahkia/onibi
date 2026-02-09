import XCTest
@testable import Onibi
final class GhosttyCliServiceTests: XCTestCase {
    func testIsGhosttyInstalledReturnsResult() async {
        let service = GhosttyCliService.shared
        let (installed, version) = await service.isGhosttyInstalled()
        // on CI/dev machine ghostty may or may not be installed; just verify no crash
        if installed {
            XCTAssertNotNil(version)
        } else {
            XCTAssertNil(version)
        }
    }
    func testCliErrorTypes() {
        let parseErr = GhosttyCliService.CliError.parseError
        XCTAssertNotNil(parseErr)
        let execErr = GhosttyCliService.CliError.executionError(NSError(domain: "test", code: 1))
        XCTAssertNotNil(execErr)
    }
}
