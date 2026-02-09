import Foundation

/// JSON-based storage implementation for logs
final class JSONStorageManager: StorageManager {
    private let logsPath: String
    private let backupPath: String
    private let fileManager = FileManager.default
    private var cache: [LogEntry] = []
    private var isDirty = false
    private var flushTimer: Timer?
    private let flushInterval: TimeInterval
    private let queue = DispatchQueue(label: "com.onibi.storage", qos: .utility)
    private let cacheLock = NSLock()
    
    /// Storage file version for migrations
    private static let currentVersion = 1
    
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
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            queue.async { [weak self] in
                guard let self = self else {
                    continuation.resume(throwing: StorageError.writeFailure(underlying: NSError(domain: "Storage", code: -1)))
                    return
                }
                do {
                    try self.writeLogsAtomic(entries)
                    self.cache = entries
                    self.isDirty = false
                    continuation.resume()
                } catch {
                    continuation.resume(throwing: StorageError.writeFailure(underlying: error))
                }
            }
        }
    }
    
    func loadLogs() async throws -> [LogEntry] {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<[LogEntry], Error>) in
            queue.async { [weak self] in
                guard let self = self else {
                    continuation.resume(returning: [])
                    return
                }
                do {
                    let logs = try self.readLogs()
                    self.cache = logs
                    continuation.resume(returning: logs)
                } catch {
                    // Try backup
                    if let backup = try? self.readLogs(from: self.backupPath) {
                        self.cache = backup
                        continuation.resume(returning: backup)
                    } else {
                        continuation.resume(returning: [])
                    }
                }
            }
        }
    }
    
    func appendLog(_ entry: LogEntry) async throws {
        cacheLock.lock()
        cache.append(entry)
        isDirty = true
        cacheLock.unlock()
    }
    
    func deleteLogsOlderThan(_ date: Date) async throws {
        cacheLock.lock()
        cache.removeAll { $0.timestamp < date }
        isDirty = true
        cacheLock.unlock()
        try await flushToDisk()
    }
    
    func clearAllLogs() async throws {
        cacheLock.lock()
        cache.removeAll()
        isDirty = false
        cacheLock.unlock()
        try fileManager.removeItem(atPath: logsPath)
    }
    
    func getStorageSize() async throws -> Int64 {
        guard fileManager.fileExists(atPath: logsPath) else { return 0 }
        let attrs = try fileManager.attributesOfItem(atPath: logsPath)
        return (attrs[.size] as? Int64) ?? 0
    }
    
    // MARK: - Batch Operations
    
    /// Append multiple logs efficiently
    func appendLogs(_ entries: [LogEntry]) async throws {
        cacheLock.lock()
        cache.append(contentsOf: entries)
        isDirty = true
        cacheLock.unlock()
    }
    
    /// Flush in-memory cache to disk
    func flushToDisk() async throws {
        cacheLock.lock()
        guard isDirty else { cacheLock.unlock(); return }
        let snapshot = cache
        cacheLock.unlock()
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
        let wrapper = try decoder.decode(StorageWrapper.self, from: data)
        
        // Handle version migration if needed
        if wrapper.version < JSONStorageManager.currentVersion {
            // Future: implement migrations
        }
        
        return wrapper.logs
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
        cacheLock.lock()
        guard isDirty else { cacheLock.unlock(); return }
        let snapshot = cache
        cacheLock.unlock()
        try writeLogsAtomic(snapshot)
        cacheLock.lock()
        isDirty = false
        cacheLock.unlock()
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
