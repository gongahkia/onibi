import Foundation
import Combine

/// Status of a managed Cloudflare Quick Tunnel spawned from the app.
struct CloudflaredStatus: Equatable {
    let isInstalled: Bool
    let isRunning: Bool
    let publicURL: String?
    let detail: String?

    static let unavailable = CloudflaredStatus(
        isInstalled: false,
        isRunning: false,
        publicURL: nil,
        detail: "cloudflared not installed"
    )
}

/// Manages a local `cloudflared tunnel --url` Quick Tunnel process. One-click install via Homebrew.
final class CloudflaredService: ObservableObject {
    static let shared = CloudflaredService()

    @Published private(set) var status: CloudflaredStatus = .unavailable
    @Published private(set) var lastError: String?
    @Published private(set) var installLog: String = ""
    @Published private(set) var isInstalling: Bool = false
    @Published private(set) var tunnelLogTail: String = ""

    private var tunnelProcess: Process?
    private var installProcess: Process?
    private let queue = DispatchQueue(label: "com.onibi.cloudflared", qos: .userInitiated)
    private let urlPattern = try? NSRegularExpression(
        pattern: "https://[a-z0-9-]+\\.trycloudflare\\.com",
        options: [.caseInsensitive]
    )

    private init() {
        refreshStatus()
    }

    // MARK: - Binary detection

    private static let brewPaths = [
        "/opt/homebrew/bin/brew",
        "/usr/local/bin/brew"
    ]

    private static let cloudflaredPaths = [
        "/opt/homebrew/bin/cloudflared",
        "/usr/local/bin/cloudflared"
    ]

    private func resolveBinary(_ candidates: [String]) -> String? {
        candidates.first(where: { FileManager.default.isExecutableFile(atPath: $0) })
    }

    var isCloudflaredInstalled: Bool {
        resolveBinary(Self.cloudflaredPaths) != nil
    }

    var isBrewInstalled: Bool {
        resolveBinary(Self.brewPaths) != nil
    }

    var brewInstallCommand: String {
        "brew install cloudflared"
    }

    // MARK: - Install

    /// Kicks off `brew install cloudflared` in the background and streams output into `installLog`.
    /// Caller should check `isBrewInstalled` first — if Homebrew itself is missing, we return early.
    func installViaBrew() {
        guard !isInstalling else { return }
        guard let brew = resolveBinary(Self.brewPaths) else {
            DispatchQueue.main.async {
                self.lastError = "Homebrew not detected. Install Homebrew first from https://brew.sh."
            }
            return
        }

        DispatchQueue.main.async {
            self.isInstalling = true
            self.installLog = ""
            self.lastError = nil
        }

        let process = Process()
        let pipe = Pipe()
        process.executableURL = URL(fileURLWithPath: brew)
        process.arguments = ["install", "cloudflared"]
        process.standardOutput = pipe
        process.standardError = pipe
        // Ensure PATH is populated for a GUI-launched process.
        var env = ProcessInfo.processInfo.environment
        env["PATH"] = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:\(env["PATH"] ?? "")"
        process.environment = env

        pipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
            guard let self else { return }
            let data = handle.availableData
            guard !data.isEmpty, let chunk = String(data: data, encoding: .utf8) else { return }
            DispatchQueue.main.async {
                self.installLog.append(chunk)
                if self.installLog.count > 16_000 {
                    self.installLog = String(self.installLog.suffix(16_000))
                }
            }
        }

        process.terminationHandler = { [weak self] proc in
            pipe.fileHandleForReading.readabilityHandler = nil
            guard let self else { return }
            let exitCode = proc.terminationStatus
            DispatchQueue.main.async {
                self.isInstalling = false
                self.installProcess = nil
                if exitCode == 0 {
                    DiagnosticsStore.shared.record(
                        component: "CloudflaredService",
                        level: .info,
                        message: "cloudflared installed via brew"
                    )
                    self.refreshStatus()
                } else {
                    let message = "brew install cloudflared exited with code \(exitCode)"
                    self.lastError = message
                    DiagnosticsStore.shared.record(
                        component: "CloudflaredService",
                        level: .error,
                        message: message
                    )
                }
            }
        }

        do {
            try process.run()
            self.installProcess = process
        } catch {
            let message = "Failed to launch brew: \(error.localizedDescription)"
            DispatchQueue.main.async {
                self.isInstalling = false
                self.installProcess = nil
                self.lastError = message
            }
            DiagnosticsStore.shared.record(
                component: "CloudflaredService",
                level: .error,
                message: message
            )
        }
    }

    /// Cancels an in-flight install.
    func cancelInstall() {
        installProcess?.terminate()
    }

    // MARK: - Tunnel lifecycle

    /// Spawns `cloudflared tunnel --url http://127.0.0.1:<port>` and watches stdout for the public URL.
    func startTunnel(port: Int) {
        guard !status.isRunning else { return }
        guard let binary = resolveBinary(Self.cloudflaredPaths) else {
            DispatchQueue.main.async {
                self.status = .unavailable
                self.lastError = "cloudflared binary not found on disk."
            }
            return
        }

        let process = Process()
        let pipe = Pipe()
        process.executableURL = URL(fileURLWithPath: binary)
        process.arguments = ["tunnel", "--url", "http://127.0.0.1:\(port)"]
        process.standardOutput = pipe
        process.standardError = pipe

        pipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
            guard let self else { return }
            let data = handle.availableData
            guard !data.isEmpty, let chunk = String(data: data, encoding: .utf8) else { return }
            self.handleTunnelOutput(chunk)
        }

        process.terminationHandler = { [weak self] proc in
            pipe.fileHandleForReading.readabilityHandler = nil
            guard let self else { return }
            let exitCode = proc.terminationStatus
            DispatchQueue.main.async {
                self.tunnelProcess = nil
                self.status = CloudflaredStatus(
                    isInstalled: self.isCloudflaredInstalled,
                    isRunning: false,
                    publicURL: nil,
                    detail: "cloudflared exited (code \(exitCode))"
                )
                if exitCode != 0 && exitCode != 15 /* SIGTERM */ {
                    self.lastError = "cloudflared terminated unexpectedly (code \(exitCode))"
                }
                DiagnosticsStore.shared.record(
                    component: "CloudflaredService",
                    level: .info,
                    message: "cloudflared tunnel exited",
                    metadata: ["exitCode": String(exitCode)]
                )
            }
        }

        do {
            try process.run()
            tunnelProcess = process
            DispatchQueue.main.async {
                self.lastError = nil
                self.status = CloudflaredStatus(
                    isInstalled: true,
                    isRunning: true,
                    publicURL: nil,
                    detail: "Starting Cloudflare Quick Tunnel…"
                )
            }
            DiagnosticsStore.shared.record(
                component: "CloudflaredService",
                level: .info,
                message: "cloudflared tunnel started",
                metadata: ["port": String(port)]
            )
        } catch {
            DispatchQueue.main.async {
                self.lastError = "Failed to start cloudflared: \(error.localizedDescription)"
            }
            DiagnosticsStore.shared.record(
                component: "CloudflaredService",
                level: .error,
                message: "cloudflared tunnel failed to start",
                metadata: ["reason": error.localizedDescription]
            )
        }
    }

    func stopTunnel() {
        tunnelProcess?.terminate()
        tunnelProcess = nil
        DispatchQueue.main.async {
            self.status = CloudflaredStatus(
                isInstalled: self.isCloudflaredInstalled,
                isRunning: false,
                publicURL: nil,
                detail: "Tunnel stopped"
            )
        }
    }

    private func handleTunnelOutput(_ chunk: String) {
        DispatchQueue.main.async {
            self.tunnelLogTail.append(chunk)
            if self.tunnelLogTail.count > 4_000 {
                self.tunnelLogTail = String(self.tunnelLogTail.suffix(4_000))
            }
        }

        guard let regex = urlPattern else { return }
        let ns = chunk as NSString
        let matches = regex.matches(in: chunk, range: NSRange(location: 0, length: ns.length))
        guard let first = matches.first else { return }
        let url = ns.substring(with: first.range)

        DispatchQueue.main.async {
            if self.status.publicURL != url {
                self.status = CloudflaredStatus(
                    isInstalled: true,
                    isRunning: true,
                    publicURL: url,
                    detail: "Serving tunnel"
                )
                DiagnosticsStore.shared.record(
                    component: "CloudflaredService",
                    level: .info,
                    message: "cloudflared public URL received",
                    metadata: ["url": url]
                )
            }
        }
    }

    /// Re-evaluates binary presence; call after install completes.
    func refreshStatus() {
        let installed = isCloudflaredInstalled
        let running = tunnelProcess?.isRunning ?? false
        DispatchQueue.main.async {
            self.status = CloudflaredStatus(
                isInstalled: installed,
                isRunning: running,
                publicURL: running ? self.status.publicURL : nil,
                detail: installed
                    ? (running ? "Serving tunnel" : "cloudflared installed")
                    : "cloudflared not installed"
            )
        }
    }
}
