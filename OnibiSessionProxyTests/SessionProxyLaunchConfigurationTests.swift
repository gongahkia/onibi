import XCTest
@testable import OnibiSessionProxy

final class SessionProxyLaunchConfigurationTests: XCTestCase {
    func testLaunchConfigurationParsesRequiredEnvironment() throws {
        let configuration = try SessionProxyLaunchConfiguration(
            environment: [
                "ONIBI_PROXY_SOCKET_PATH": "/tmp/onibi.sock",
                "ONIBI_HOST_SESSION_ID": "session-1",
                "ONIBI_PARENT_SHELL": "/bin/zsh",
                "ONIBI_PARENT_SHELL_ARGS": "-l",
                "ONIBI_PROXY_VERSION": "1.0.0",
                "PWD": "/tmp/project"
            ]
        )

        XCTAssertEqual(configuration.socketPath, "/tmp/onibi.sock")
        XCTAssertEqual(configuration.sessionId, "session-1")
        XCTAssertEqual(configuration.shellPath, "/bin/zsh")
        XCTAssertEqual(configuration.shellArguments, ["-l"])
        XCTAssertEqual(configuration.version, "1.0.0")
        XCTAssertEqual(configuration.workingDirectory, "/tmp/project")
    }

    func testLaunchConfigurationRejectsMissingSocketPath() {
        XCTAssertThrowsError(
            try SessionProxyLaunchConfiguration(
                environment: [
                    "ONIBI_HOST_SESSION_ID": "session-1",
                    "ONIBI_PARENT_SHELL": "/bin/zsh"
                ]
            )
        )
    }

    func testFallbackLaunchContextPrefersOnibiParentShell() {
        let context = SessionProxyRuntime.fallbackLaunchContext(
            environment: [
                "ONIBI_PARENT_SHELL": "/bin/bash",
                "ONIBI_PARENT_SHELL_ARGS": "--login",
                "SHELL": "/bin/zsh"
            ]
        )

        XCTAssertEqual(context.shellPath, "/bin/bash")
        XCTAssertEqual(context.shellArguments, ["--login"])
    }

    func testFallbackLaunchContextFallsBackToShellEnvironment() {
        let context = SessionProxyRuntime.fallbackLaunchContext(
            environment: [
                "SHELL": "/opt/homebrew/bin/fish",
                "ONIBI_PARENT_SHELL_ARGS": "--interactive"
            ]
        )

        XCTAssertEqual(context.shellPath, "/opt/homebrew/bin/fish")
        XCTAssertEqual(context.shellArguments, ["--interactive"])
    }

    func testFallbackLaunchContextUsesDefaultShellWhenUnset() {
        let context = SessionProxyRuntime.fallbackLaunchContext(environment: [:])
        XCTAssertEqual(context.shellPath, "/bin/zsh")
        XCTAssertEqual(context.shellArguments, [])
    }
}
