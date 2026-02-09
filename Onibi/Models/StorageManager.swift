import Foundation

/// Protocol for log persistence backends
protocol StorageManager {
    /// Save log entries to storage
    func saveLogs(_ entries: [LogEntry]) async throws
    
    /// Load all log entries from storage
    func loadLogs() async throws -> [LogEntry]
    
    /// Append a single log entry
    func appendLog(_ entry: LogEntry) async throws
    
    /// Delete log entries older than the specified date
    func deleteLogsOlderThan(_ date: Date) async throws
    
    /// Clear all stored logs
    func clearAllLogs() async throws
    
    /// Get current storage size in bytes
    func getStorageSize() async throws -> Int64
}

/// Errors that can occur during storage operations
enum StorageError: Error, LocalizedError {
    case fileNotFound
    case corruptedData
    case writeFailure(underlying: Error)
    case readFailure(underlying: Error)
    case storageLimitExceeded
    
    var errorDescription: String? {
        switch self {
        case .fileNotFound:
            return "Storage file not found"
        case .corruptedData:
            return "Storage data is corrupted"
        case .writeFailure(let error):
            return "Failed to write to storage: \(error.localizedDescription)"
        case .readFailure(let error):
            return "Failed to read from storage: \(error.localizedDescription)"
        case .storageLimitExceeded:
            return "Storage limit exceeded"
        }
    }
}
