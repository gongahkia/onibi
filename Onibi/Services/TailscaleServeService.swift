import Foundation

struct TailscaleServeStatus: Equatable {
    let isInstalled: Bool
    let isServing: Bool
    let baseURLString: String?
    let detail: String?

    static let unavailable = TailscaleServeStatus(
        isInstalled: false,
        isServing: false,
        baseURLString: nil,
        detail: "Tailscale CLI not installed"
    )
}

final class TailscaleServeService {
    static let shared = TailscaleServeService()

    private init() {}

    func refreshStatus(port: Int) async -> TailscaleServeStatus {
        guard let binaryPath = resolveBinaryPath() else {
            DiagnosticsStore.shared.record(
                component: "TailscaleServeService",
                level: .info,
                message: "tailscale binary unavailable"
            )
            return .unavailable
        }

        let statusOutput: String?
        let serveOutput: String?
        var statusCommandError: Error?
        var serveCommandError: Error?

        do {
            statusOutput = try await runProcess(binaryPath: binaryPath, arguments: ["status", "--json"])
        } catch {
            statusOutput = nil
            statusCommandError = error
            DiagnosticsStore.shared.record(
                component: "TailscaleServeService",
                level: .warning,
                message: "tailscale status command failed",
                metadata: [
                    "reason": error.localizedDescription
                ]
            )
        }

        do {
            serveOutput = try await runProcess(binaryPath: binaryPath, arguments: ["serve", "status", "--json"])
        } catch {
            serveOutput = nil
            serveCommandError = error
            DiagnosticsStore.shared.record(
                component: "TailscaleServeService",
                level: .warning,
                message: "tailscale serve status command failed",
                metadata: [
                    "reason": error.localizedDescription
                ]
            )
        }

        let dnsName = parseDNSName(from: statusOutput)
        let isServing = parseServeEnabled(from: serveOutput)
        let detail: String
        if let serveCommandError {
            detail = "Tailscale available, but `tailscale serve status` failed: \(serveCommandError.localizedDescription)"
        } else if let statusCommandError {
            detail = "Tailscale serve checked, but `tailscale status` failed: \(statusCommandError.localizedDescription)"
        } else {
            detail = isServing
                ? "Serving local port \(port) over Tailscale"
                : "Tailscale available but Serve is not active"
        }

        return TailscaleServeStatus(
            isInstalled: true,
            isServing: isServing,
            baseURLString: dnsName.map { "https://\($0)" },
            detail: detail
        )
    }

    func enableServe(port: Int) async throws -> TailscaleServeStatus {
        guard let binaryPath = resolveBinaryPath() else {
            DiagnosticsStore.shared.record(
                component: "TailscaleServeService",
                level: .warning,
                message: "cannot enable tailscale serve because binary is missing"
            )
            return .unavailable
        }

        do {
            _ = try await runProcess(binaryPath: binaryPath, arguments: ["serve", "--bg", String(port)])
        } catch {
            DiagnosticsStore.shared.record(
                component: "TailscaleServeService",
                level: .error,
                message: "tailscale serve enable failed",
                metadata: [
                    "reason": error.localizedDescription,
                    "port": String(port)
                ]
            )
            throw error
        }
        return await refreshStatus(port: port)
    }

    func disableServe() async {
        guard let binaryPath = resolveBinaryPath() else { return }
        do {
            _ = try await runProcess(binaryPath: binaryPath, arguments: ["serve", "reset"])
        } catch {
            DiagnosticsStore.shared.record(
                component: "TailscaleServeService",
                level: .warning,
                message: "tailscale serve reset failed",
                metadata: [
                    "reason": error.localizedDescription
                ]
            )
        }
    }

    private func resolveBinaryPath() -> String? {
        let candidates = [
            "/usr/local/bin/tailscale",
            "/opt/homebrew/bin/tailscale",
            "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
        ]

        return candidates.first(where: { FileManager.default.isExecutableFile(atPath: $0) })
    }

    private func runProcess(binaryPath: String, arguments: [String]) async throws -> String {
        try await withCheckedThrowingContinuation { continuation in
            let process = Process()
            let outputPipe = Pipe()
            let errorPipe = Pipe()

            process.executableURL = URL(fileURLWithPath: binaryPath)
            process.arguments = arguments
            process.standardOutput = outputPipe
            process.standardError = errorPipe

            process.terminationHandler = { process in
                let outputData = outputPipe.fileHandleForReading.readDataToEndOfFile()
                let errorData = errorPipe.fileHandleForReading.readDataToEndOfFile()
                let output = String(data: outputData, encoding: .utf8) ?? ""
                let errorOutput = String(data: errorData, encoding: .utf8) ?? ""

                if process.terminationStatus == 0 {
                    continuation.resume(returning: output)
                } else {
                    continuation.resume(throwing: NSError(
                        domain: "TailscaleServeService",
                        code: Int(process.terminationStatus),
                        userInfo: [NSLocalizedDescriptionKey: errorOutput.isEmpty ? output : errorOutput]
                    ))
                }
            }

            do {
                try process.run()
            } catch {
                continuation.resume(throwing: error)
            }
        }
    }

    private func parseDNSName(from output: String?) -> String? {
        guard let output, let data = output.data(using: .utf8) else {
            return nil
        }
        let json: [String: Any]
        do {
            json = try JSONSerialization.jsonObject(with: data) as? [String: Any] ?? [:]
        } catch {
            DiagnosticsStore.shared.record(
                component: "TailscaleServeService",
                level: .warning,
                message: "failed to parse tailscale status JSON",
                metadata: [
                    "reason": error.localizedDescription
                ]
            )
            return nil
        }

        if
            let selfNode = json["Self"] as? [String: Any],
            let dnsName = selfNode["DNSName"] as? String
        {
            return dnsName.trimmingCharacters(in: CharacterSet(charactersIn: "."))
        }

        if let dnsName = json["CurrentTailnet"] as? [String: Any], let magicDNS = dnsName["MagicDNSSuffix"] as? String {
            return magicDNS.trimmingCharacters(in: CharacterSet(charactersIn: "."))
        }

        return nil
    }

    private func parseServeEnabled(from output: String?) -> Bool {
        guard let output else { return false }
        let trimmed = output.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !trimmed.isEmpty, trimmed != "{}" else { return false }
        return true
    }
}
