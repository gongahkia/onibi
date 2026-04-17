import Foundation
import AppKit

struct DiagnosticsBundle: Encodable {
    let generatedAt: Date
    let appVersion: String
    let appBuild: String
    let gatewayRunning: Bool
    let bindMode: String
    let advertisedURLs: [String]
    let recentEvents: [Entry]
    let redactedSettings: [String: String]
    let platform: String

    struct Entry: Encodable {
        let timestamp: Date
        let component: String
        let level: String
        let message: String
        let metadata: [String: String]
    }
}

enum DiagnosticsBundleBuilder {
    /// Keys whose values must never leave the device in plaintext.
    static let sensitiveKeys: Set<String> = [
        "pairingToken",
        "authorization",
        "Authorization",
        "token",
        "bearer"
    ]

    /// Returns a redacted copy of the value; never mutates input.
    static func redactValue(_ raw: String) -> String {
        guard raw.count > 4 else { return "***" }
        return String(raw.prefix(4)) + "…(\(raw.count))"
    }

    static func redactSettings(_ settings: AppSettings) -> [String: String] {
        return [
            "mobileAccessEnabled": String(settings.mobileAccessEnabled),
            "mobileAccessPort": String(settings.mobileAccessPort),
            "mobileAccessBindMode": settings.mobileAccessBindMode.rawValue,
            "mobileAccessTunnelURL": settings.mobileAccessTunnelURL,
            "remoteControlEnabled": String(settings.remoteControlEnabled),
            "sessionProxySocketPath": settings.sessionProxySocketPath,
            "logRetentionDays": String(settings.logRetentionDays),
            "detectionThreshold": String(settings.detectionThreshold),
            "userPersona": settings.userPersona.rawValue
        ]
    }

    @MainActor
    static func build() -> DiagnosticsBundle {
        let settings = SettingsViewModel.shared.settings
        let gateway = MobileGatewayService.shared
        let events = DiagnosticsStore.shared.recentEvents(limit: 200).map { event in
            DiagnosticsBundle.Entry(
                timestamp: event.timestamp,
                component: event.component,
                level: event.level.rawValue,
                message: event.message,
                metadata: event.metadata.reduce(into: [String: String]()) { acc, kv in
                    acc[kv.key] = sensitiveKeys.contains(kv.key) ? redactValue(kv.value) : kv.value
                }
            )
        }

        let infoDict = Bundle.main.infoDictionary
        let version = infoDict?["CFBundleShortVersionString"] as? String ?? "dev"
        let build = infoDict?["CFBundleVersion"] as? String ?? "0"

        return DiagnosticsBundle(
            generatedAt: Date(),
            appVersion: version,
            appBuild: build,
            gatewayRunning: gateway.isRunning,
            bindMode: settings.mobileAccessBindMode.rawValue,
            advertisedURLs: gateway.advertisedURLs,
            recentEvents: events,
            redactedSettings: redactSettings(settings),
            platform: ProcessInfo.processInfo.operatingSystemVersionString
        )
    }

    static func encode(_ bundle: DiagnosticsBundle) throws -> Data {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        encoder.dateEncodingStrategy = .iso8601
        return try encoder.encode(bundle)
    }

    @MainActor
    static func writeToDownloads() throws -> URL {
        let bundle = build()
        let data = try encode(bundle)
        let filename = "onibi-diagnostics-\(Int(Date().timeIntervalSince1970)).json"
        let downloads = FileManager.default.urls(for: .downloadsDirectory, in: .userDomainMask).first
            ?? URL(fileURLWithPath: NSHomeDirectory() + "/Downloads")
        let target = downloads.appendingPathComponent(filename)
        try data.write(to: target, options: .atomic)
        return target
    }
}
