import Foundation

/// JSON-based storage implementation for logs
final class JSONStorageManager: StorageManager {
    static let shared = JSONStorageManager()

    private let logsPath: String
    private let backupPath: String
    private let fileManager = FileManager.default
    private var cache: [LogEntry] = []
    private var isDirty = false
    private var flushTimer: Timer?
    private let flushInterval: TimeInterval
    private let queue = DispatchQueue(label: "com.onibi.storage", qos: .utility)
    
    /// Storage file version for migrations
    private static let currentVersion = 2
    
    init(flushInterval: TimeInterval = 30.0) {
        self.logsPath = OnibiConfig.appDataDirectory + "/logs.json"
        self.backupPath = OnibiConfig.appDataDirectory + "/logs.backup.json"
        self.flushInterval = flushInterval
        
        // Ensure directory exists
        do {
            try OnibiConfig.ensureDirectoryExists()
        } catch {
            Log.storage.error("failed to create storage directory: \(error.localizedDescription, privacy: .public)")
            DiagnosticsStore.shared.record(
                component: "JSONStorageManager",
                level: .critical,
                message: "failed to initialize storage directory",
                metadata: [
                    "reason": error.localizedDescription
                ]
            )
            ErrorReporter.shared.report(
                error,
                context: "JSONStorageManager.init.ensureDirectoryExists",
                severity: .critical
            )
        }
        
        // Start periodic flush
        startPeriodicFlush()
    }
    
    deinit {
        flushTimer?.invalidate()
        // Final synchronous flush
        do {
            try flushToDiskSync()
        } catch {
            Log.storage.error("failed to flush logs during deinit: \(error.localizedDescription, privacy: .public)")
            DiagnosticsStore.shared.record(
                component: "JSONStorageManager",
                level: .error,
                message: "failed to flush logs during deinit",
                metadata: [
                    "reason": error.localizedDescription
                ]
            )
        }
    }
    
    // MARK: - StorageManager Protocol
    
    func saveLogs(_ entries: [LogEntry]) async throws {
        do {
            try queue.sync {
                try writeLogsAtomic(entries)
                cache = entries
                isDirty = false
            }
        } catch {
            throw StorageError.writeFailure(underlying: error)
        }
    }
    
    func loadLogs() async throws -> [LogEntry] {
        do {
            return try queue.sync {
                let logs = try readLogs()
                cache = logs
                return logs
            }
        } catch {
            ErrorReporter.shared.report(error, context: "JSONStorageManager.loadLogs", severity: .warning)
            DiagnosticsStore.shared.record(
                component: "JSONStorageManager",
                level: .warning,
                message: "failed to read primary logs; attempting backup",
                metadata: [
                    "reason": error.localizedDescription
                ]
            )
            do {
                let backup = try queue.sync(execute: { try readLogs(from: backupPath) })
                queue.sync {
                    cache = backup
                }
                return backup
            } catch {
                Log.storage.error("failed to recover logs from backup: \(error.localizedDescription, privacy: .public)")
                DiagnosticsStore.shared.record(
                    component: "JSONStorageManager",
                    level: .error,
                    message: "backup log recovery failed",
                    metadata: [
                        "reason": error.localizedDescription
                    ]
                )
            }
            return []
        }
    }
    
    func appendLog(_ entry: LogEntry) async throws {
        queue.sync {
            cache.append(entry)
            isDirty = true
        }
    }
    
    func deleteLogsOlderThan(_ date: Date) async throws {
        queue.sync {
            cache.removeAll { $0.sortTimestamp < date }
            isDirty = true
        }
        try await flushToDisk()
    }
    
    func clearAllLogs() async throws {
        queue.sync {
            cache.removeAll()
            isDirty = false
        }
        if fileManager.fileExists(atPath: logsPath) {
            try fileManager.removeItem(atPath: logsPath)
        }
        if fileManager.fileExists(atPath: backupPath) {
            do {
                try fileManager.removeItem(atPath: backupPath)
            } catch {
                Log.storage.error("failed to remove backup logs during clear: \(error.localizedDescription, privacy: .public)")
                DiagnosticsStore.shared.record(
                    component: "JSONStorageManager",
                    level: .warning,
                    message: "failed to remove backup logs during clear",
                    metadata: [
                        "reason": error.localizedDescription
                    ]
                )
            }
        }
    }
    
    func getStorageSize() async throws -> Int64 {
        guard fileManager.fileExists(atPath: logsPath) else { return 0 }
        let attrs = try fileManager.attributesOfItem(atPath: logsPath)
        return (attrs[.size] as? Int64) ?? 0
    }
    
    // MARK: - Batch Operations
    
    /// Append multiple logs efficiently
    func appendLogs(_ entries: [LogEntry]) async throws {
        queue.sync {
            cache.append(contentsOf: entries)
            isDirty = true
        }
    }
    
    /// Flush in-memory cache to disk
    func flushToDisk() async throws {
        let snapshot = queue.sync { () -> [LogEntry]? in
            guard isDirty else { return nil }
            return cache
        }
        guard let snapshot else { return }
        try await saveLogs(snapshot)
    }
    
    // MARK: - Private
    
    private func startPeriodicFlush() {
        flushTimer = Timer.scheduledTimer(withTimeInterval: flushInterval, repeats: true) { [weak self] _ in
            Task {
                guard let self else { return }
                do {
                    try await self.flushToDisk()
                } catch {
                    Log.storage.error("periodic flush failed: \(error.localizedDescription, privacy: .public)")
                    DiagnosticsStore.shared.record(
                        component: "JSONStorageManager",
                        level: .warning,
                        message: "periodic flush failed",
                        metadata: [
                            "reason": error.localizedDescription
                        ]
                    )
                    ErrorReporter.shared.report(
                        error,
                        context: "JSONStorageManager.startPeriodicFlush",
                        severity: .warning
                    )
                }
            }
        }
    }
    
    private func readLogs(from path: String? = nil) throws -> [LogEntry] {
        let filePath = path ?? logsPath
        
        guard fileManager.fileExists(atPath: filePath) else {
            return []
        }
        
        let url = URL(fileURLWithPath: filePath)
        let data = try Data(contentsOf: url)
        
        // Validate JSON
        let jsonObject = try JSONSerialization.jsonObject(with: data)
        guard JSONSerialization.isValidJSONObject(jsonObject) else {
            throw StorageError.corruptedData
        }
        
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601

        do {
            let wrapper = try decoder.decode(StorageWrapper.self, from: data)
            return wrapper.logs
        } catch {
            Log.storage.debug("failed to decode storage wrapper, attempting legacy format")
        }

        do {
            let legacyLogs = try decoder.decode([LogEntry].self, from: data)
            return legacyLogs
        } catch {
            DiagnosticsStore.shared.record(
                component: "JSONStorageManager",
                level: .warning,
                message: "failed to decode logs in both modern and legacy formats",
                metadata: [
                    "path": filePath,
                    "reason": error.localizedDescription
                ]
            )
        }
        
        throw StorageError.corruptedData
    }
    
    private func writeLogsAtomic(_ logs: [LogEntry]) throws {
        // Create backup of existing file
        if fileManager.fileExists(atPath: logsPath) {
            do {
                if fileManager.fileExists(atPath: backupPath) {
                    try fileManager.removeItem(atPath: backupPath)
                }
                try fileManager.copyItem(atPath: logsPath, toPath: backupPath)
            } catch {
                Log.storage.error("failed to update logs backup: \(error.localizedDescription, privacy: .public)")
                DiagnosticsStore.shared.record(
                    component: "JSONStorageManager",
                    level: .warning,
                    message: "failed to update storage backup before write",
                    metadata: [
                        "reason": error.localizedDescription
                    ]
                )
            }
        }
        
        let wrapper = StorageWrapper(version: JSONStorageManager.currentVersion, logs: logs)
        
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        encoder.dateEncodingStrategy = .iso8601
        let data = try encoder.encode(wrapper)
        
        // Write to temp file first
        let tempPath = logsPath + ".tmp"
        let tempURL = URL(fileURLWithPath: tempPath)
        try data.write(to: tempURL, options: .atomic)
        
        // Rename temp to actual (atomic operation)
        if fileManager.fileExists(atPath: logsPath) {
            do {
                try fileManager.removeItem(atPath: logsPath)
            } catch {
                Log.storage.error("failed removing previous logs before atomic move: \(error.localizedDescription, privacy: .public)")
                DiagnosticsStore.shared.record(
                    component: "JSONStorageManager",
                    level: .warning,
                    message: "failed removing previous logs before atomic move",
                    metadata: [
                        "reason": error.localizedDescription
                    ]
                )
            }
        }
        try fileManager.moveItem(atPath: tempPath, toPath: logsPath)
    }
    
    private func flushToDiskSync() throws {
        let snapshot = queue.sync { () -> [LogEntry]? in
            guard isDirty else { return nil }
            return cache
        }
        guard let snapshot else { return }
        try writeLogsAtomic(snapshot)
        queue.sync {
            isDirty = false
        }
    }
}

// MARK: - Storage Wrapper

/// Wrapper for versioned storage
private struct StorageWrapper: Codable {
    let version: Int
    let logs: [LogEntry]
}

// MARK: - Settings Storage

/// Extension for settings persistence using UserDefaults
extension UserDefaults {
    private enum Keys {
        static let settings = "com.onibi.app.settings"
    }
    
    func saveSettings(_ settings: AppSettings) throws {
        let encoder = JSONEncoder()
        let data = try encoder.encode(settings)
        set(data, forKey: Keys.settings)
    }
    
    func loadSettings() -> AppSettings? {
        guard let data = data(forKey: Keys.settings) else { return nil }
        do {
            return try JSONDecoder().decode(AppSettings.self, from: data)
        } catch {
            Log.settings.error("failed to decode settings from UserDefaults: \(error.localizedDescription, privacy: .public)")
            DiagnosticsStore.shared.record(
                component: "UserDefaults",
                level: .warning,
                message: "failed to decode persisted settings",
                metadata: [
                    "reason": error.localizedDescription
                ]
            )
            return nil
        }
    }
}
