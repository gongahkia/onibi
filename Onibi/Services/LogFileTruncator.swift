import Foundation

/// Service for managing log file size through truncation and rotation
final class LogFileTruncator: ObservableObject {
    static let shared = LogFileTruncator()
    
    @Published var currentLogSizeBytes: Int64 = 0
    @Published var lastTruncationDate: Date?
    
    private let fileManager = FileManager.default
    private var monitorTimer: Timer?
    
    private init() {}
    
    deinit {
        stopMonitoring()
    }
    
    // MARK: - Monitoring
    
    /// Start periodic log size monitoring
    func startMonitoring(intervalSeconds: TimeInterval = 60) {
        stopMonitoring()
        updateCurrentSize()
        
        monitorTimer = Timer.scheduledTimer(withTimeInterval: intervalSeconds, repeats: true) { [weak self] _ in
            self?.checkAndTruncateIfNeeded()
        }
    }
    
    /// Stop monitoring
    func stopMonitoring() {
        monitorTimer?.invalidate()
        monitorTimer = nil
    }
    
    /// Update current log file size
    func updateCurrentSize() {
        let logPath = SettingsViewModel.shared.settings.logFilePath
        
        if let attributes = try? fileManager.attributesOfItem(atPath: logPath),
           let size = attributes[.size] as? Int64 {
            DispatchQueue.main.async { [weak self] in
                self?.currentLogSizeBytes = size
            }
        } else {
            DispatchQueue.main.async { [weak self] in
                self?.currentLogSizeBytes = 0
            }
        }
    }
    
    // MARK: - Truncation
    
    /// Check if truncation is needed and perform if so
    func checkAndTruncateIfNeeded() {
        updateCurrentSize()
        
        let settings = SettingsViewModel.shared.settings
        let maxSizeBytes = Int64(settings.maxLogFileSizeMB) * 1024 * 1024
        
        if currentLogSizeBytes > maxSizeBytes {
            do {
                try rotateAndTruncate(keepLines: settings.maxLogLines)
            } catch {
                print("[LogFileTruncator] Error during truncation: \(error)")
            }
        }
    }
    
    /// Rotate log file and truncate to keep only last N lines
    func rotateAndTruncate(keepLines: Int) throws {
        let logPath = SettingsViewModel.shared.settings.logFilePath
        
        guard fileManager.fileExists(atPath: logPath) else { return }
        
        // Perform rotation first
        try rotateLogFiles()
        
        // Read current log
        let content = try String(contentsOfFile: logPath, encoding: .utf8)
        let lines = content.components(separatedBy: .newlines)
        
        // Keep only last N lines
        let linesToKeep = Array(lines.suffix(keepLines))
        let truncatedContent = linesToKeep.joined(separator: "\n")
        
        // Write truncated content
        try truncatedContent.write(toFile: logPath, atomically: true, encoding: .utf8)
        
        DispatchQueue.main.async { [weak self] in
            self?.lastTruncationDate = Date()
        }
        
        updateCurrentSize()
    }
    
    /// Rotate log files: terminal.log -> terminal.log.1 -> terminal.log.2 -> terminal.log.3 (max 3)
    func rotateLogFiles() throws {
        let basePath = SettingsViewModel.shared.settings.logFilePath
        let maxRotations = 3
        
        // Remove oldest rotation if exists
        let oldestPath = "\(basePath).\(maxRotations)"
        if fileManager.fileExists(atPath: oldestPath) {
            try fileManager.removeItem(atPath: oldestPath)
        }
        
        // Shift existing rotations
        for i in stride(from: maxRotations - 1, through: 1, by: -1) {
            let currentPath = "\(basePath).\(i)"
            let nextPath = "\(basePath).\(i + 1)"
            
            if fileManager.fileExists(atPath: currentPath) {
                try fileManager.moveItem(atPath: currentPath, toPath: nextPath)
            }
        }
        
        // Move current log to .1
        if fileManager.fileExists(atPath: basePath) {
            try fileManager.copyItem(atPath: basePath, toPath: "\(basePath).1")
        }
    }
    
    // MARK: - Size Utilities
    
    /// Get human-readable size string
    var formattedSize: String {
        ByteCountFormatter.string(fromByteCount: currentLogSizeBytes, countStyle: .file)
    }
    
    /// Get size as percentage of max allowed
    func sizePercentage(maxMB: Int) -> Double {
        let maxBytes = Double(maxMB) * 1024 * 1024
        return min(Double(currentLogSizeBytes) / maxBytes, 1.0)
    }
    
    /// Check if storage is getting low (>80% of max)
    func isStorageLow(maxMB: Int) -> Bool {
        sizePercentage(maxMB: maxMB) > 0.8
    }
    
    /// Get total size of logs directory
    func totalLogsDirectorySize() -> Int64 {
        let logsDir = OnibiConfig.appDataDirectory
        var totalSize: Int64 = 0
        
        if let enumerator = fileManager.enumerator(atPath: logsDir) {
            while let file = enumerator.nextObject() as? String {
                let fullPath = (logsDir as NSString).appendingPathComponent(file)
                if let attributes = try? fileManager.attributesOfItem(atPath: fullPath),
                   let size = attributes[.size] as? Int64 {
                    totalSize += size
                }
            }
        }
        
        return totalSize
    }
    
    /// Get list of rotated log files
    func getRotatedLogFiles() -> [URL] {
        let basePath = SettingsViewModel.shared.settings.logFilePath
        var files: [URL] = []
        
        for i in 1...3 {
            let path = "\(basePath).\(i)"
            if fileManager.fileExists(atPath: path) {
                files.append(URL(fileURLWithPath: path))
            }
        }
        
        return files
    }
    
    /// Delete all rotated log files
    func deleteRotatedLogs() throws {
        for url in getRotatedLogFiles() {
            try fileManager.removeItem(at: url)
        }
    }
}
