import Foundation

final class SessionProxyCoordinator {
    static let shared = SessionProxyCoordinator()

    private init() {}

    func proxyVersion() -> String {
        Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "dev"
    }

    func shellArguments(for shell: ShellHookInstaller.Shell) -> [String] {
        switch shell {
        case .zsh:
            return ["-l"]
        case .bash:
            return ["--login"]
        case .fish:
            return ["--interactive"]
        }
    }

    func resolvedProxyBinaryPath(fileManager: FileManager = .default) -> String? {
        if
            let overridePath = ProcessInfo.processInfo.environment["ONIBI_PROXY_BINARY"],
            fileManager.isExecutableFile(atPath: overridePath)
        {
            return overridePath
        }

        let currentDirectoryURL = URL(fileURLWithPath: fileManager.currentDirectoryPath)
        var candidateURLs: [URL] = [
            currentDirectoryURL.appendingPathComponent(".build/debug/OnibiSessionProxy"),
            currentDirectoryURL.appendingPathComponent(".build/release/OnibiSessionProxy")
        ]

        if let executableURL = Bundle.main.executableURL {
            let executableDirectory = executableURL.deletingLastPathComponent()
            candidateURLs.append(executableDirectory.appendingPathComponent("OnibiSessionProxy"))
            candidateURLs.append(executableDirectory.deletingLastPathComponent().appendingPathComponent("OnibiSessionProxy"))
            candidateURLs.append(
                executableDirectory
                    .appendingPathComponent("../OnibiSessionProxy")
                    .standardizedFileURL
            )
        }

        return candidateURLs.first { fileManager.isExecutableFile(atPath: $0.path) }?.path
    }

    func hookScript(for shell: ShellHookInstaller.Shell, settings: AppSettings) -> String {
        shell.hookScript(
            logPath: settings.logFilePath,
            remoteControlEnabled: settings.remoteControlEnabled,
            proxyBinaryPath: resolvedProxyBinaryPath(),
            proxySocketPath: settings.sessionProxySocketPath,
            proxyVersion: proxyVersion(),
            shellArguments: shellArguments(for: shell)
        )
    }
}
