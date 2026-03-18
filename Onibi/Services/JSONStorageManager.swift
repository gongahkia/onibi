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
        try? OnibiConfig.ensureDirectoryExists()
        
        // Start periodic flush
        startPeriodicFlush()
    }
    
    deinit {
        flushTimer?.invalidate()
        // Final synchronous flush
        try? flushToDiskSync()
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
            if let backup = try? queue.sync(execute: { try readLogs(from: backupPath) }) {
                queue.sync {
                    cache = backup
                }
                return backup
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
            try? fileManager.removeItem(atPath: backupPath)
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
                try? await self?.flushToDisk()
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

        if let wrapper = try? decoder.decode(StorageWrapper.self, from: data) {
            return wrapper.logs
        }

        if let legacyLogs = try? decoder.decode([LogEntry].self, from: data) {
            return legacyLogs
        }
        
        throw StorageError.corruptedData
    }
    
    private func writeLogsAtomic(_ logs: [LogEntry]) throws {
        // Create backup of existing file
        if fileManager.fileExists(atPath: logsPath) {
            try? fileManager.removeItem(atPath: backupPath)
            try? fileManager.copyItem(atPath: logsPath, toPath: backupPath)
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
        try? fileManager.removeItem(atPath: logsPath)
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
        return try? JSONDecoder().decode(AppSettings.self, from: data)
    }
}
