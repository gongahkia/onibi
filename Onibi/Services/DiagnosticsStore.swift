import Foundation

enum DiagnosticsLevel: String, Codable, CaseIterable, Sendable {
    case debug
    case info
    case warning
    case error
    case critical
}

struct DiagnosticsEvent: Identifiable, Codable, Sendable {
    let id: UUID
    let timestamp: Date
    let component: String
    let level: DiagnosticsLevel
    let message: String
    let metadata: [String: String]

    init(
        id: UUID = UUID(),
        timestamp: Date = Date(),
        component: String,
        level: DiagnosticsLevel,
        message: String,
        metadata: [String: String] = [:]
    ) {
        self.id = id
        self.timestamp = timestamp
        self.component = component
        self.level = level
        self.message = message
        self.metadata = metadata
    }
}

/// Central diagnostics sink for runtime observability.
final class DiagnosticsStore: ObservableObject {
    static let shared = DiagnosticsStore()

    @Published private(set) var events: [DiagnosticsEvent] = []

    private let lock = NSLock()
    private let ioQueue = DispatchQueue(label: "com.onibi.diagnostics.io", qos: .utility)
    private let maxEvents = 500
    private let encoder: JSONEncoder
    private let diagnosticsLogPath = OnibiConfig.appDataDirectory + "/diagnostics.log"

    private init() {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        encoder.outputFormatting = [.sortedKeys]
        self.encoder = encoder

        do {
            try OnibiConfig.ensureDirectoryExists()
        } catch {
            Log.diagnostics.error("failed to create diagnostics directory: \(error.localizedDescription, privacy: .public)")
        }
    }

    func record(
        component: String,
        level: DiagnosticsLevel,
        message: String,
        metadata: [String: String] = [:]
    ) {
        let event = DiagnosticsEvent(
            component: component,
            level: level,
            message: message,
            metadata: metadata
        )

        lock.lock()
        events.insert(event, at: 0)
        if events.count > maxEvents {
            events.removeLast(events.count - maxEvents)
        }
        lock.unlock()

        emitToOSLog(event)
        persist(event)
    }

    func recentEvents(limit: Int) -> [DiagnosticsEvent] {
        lock.lock()
        defer { lock.unlock() }
        let boundedLimit = max(0, min(limit, events.count))
        return Array(events.prefix(boundedLimit))
    }

    func count(for level: DiagnosticsLevel) -> Int {
        lock.lock()
        defer { lock.unlock() }
        return events.filter { $0.level == level }.count
    }

    func totalEventCount() -> Int {
        lock.lock()
        defer { lock.unlock() }
        return events.count
    }

    private func emitToOSLog(_ event: DiagnosticsEvent) {
        let metadataString = event.metadata
            .sorted(by: { $0.key < $1.key })
            .map { "\($0.key)=\($0.value)" }
            .joined(separator: " ")
        let payload = "[\(event.component)] \(event.message) \(metadataString)"

        switch event.level {
        case .debug:
            Log.diagnostics.debug("\(payload, privacy: .public)")
        case .info:
            Log.diagnostics.info("\(payload, privacy: .public)")
        case .warning:
            Log.diagnostics.warning("\(payload, privacy: .public)")
        case .error, .critical:
            Log.diagnostics.error("\(payload, privacy: .public)")
        }
    }

    private func persist(_ event: DiagnosticsEvent) {
        ioQueue.async { [weak self] in
            guard let self else { return }

            do {
                try OnibiConfig.ensureDirectoryExists()
                let encoded = try self.encoder.encode(event)
                guard var line = String(data: encoded, encoding: .utf8) else {
                    throw NSError(
                        domain: "DiagnosticsStore",
                        code: 1,
                        userInfo: [NSLocalizedDescriptionKey: "failed to encode diagnostics event as UTF-8 string"]
                    )
                }
                line.append("\n")
                guard let data = line.data(using: .utf8) else {
                    throw NSError(
                        domain: "DiagnosticsStore",
                        code: 2,
                        userInfo: [NSLocalizedDescriptionKey: "failed to encode diagnostics line as UTF-8 data"]
                    )
                }

                if FileManager.default.fileExists(atPath: self.diagnosticsLogPath) {
                    let handle = try FileHandle(forWritingTo: URL(fileURLWithPath: self.diagnosticsLogPath))
                    defer {
                        do {
                            try handle.close()
                        } catch {
                            Log.diagnostics.error("failed to close diagnostics log handle: \(error.localizedDescription, privacy: .public)")
                        }
                    }
                    try handle.seekToEnd()
                    try handle.write(contentsOf: data)
                } else {
                    try data.write(to: URL(fileURLWithPath: self.diagnosticsLogPath), options: .atomic)
                }
            } catch {
                Log.diagnostics.error("failed to persist diagnostics event: \(error.localizedDescription, privacy: .public)")
            }
        }
    }
}
