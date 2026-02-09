import Foundation
import AppKit

/// Service for error reporting and GitHub issue integration
final class ErrorReporter: ObservableObject {
    static let shared = ErrorReporter()
    
    @Published var recentErrors: [AppError] = []
    @Published var hasUnreadErrors: Bool = false
    
    private let maxErrors = 50
    private let errorLogPath = OnibiConfig.appDataDirectory + "/error.log"
    
    // GitHub repo info
    private let githubRepo = "onibi/onibi"
    
    private init() {
        loadRecentErrors()
    }
    
    // MARK: - Error Model
    
    struct AppError: Identifiable, Codable {
        let id: UUID
        let timestamp: Date
        let title: String
        let message: String
        let context: String?
        let stackTrace: String?
        let severity: Severity
        
        enum Severity: String, Codable {
            case info
            case warning
            case error
            case critical
        }
        
        init(
            id: UUID = UUID(),
            timestamp: Date = Date(),
            title: String,
            message: String,
            context: String? = nil,
            stackTrace: String? = nil,
            severity: Severity = .error
        ) {
            self.id = id
            self.timestamp = timestamp
            self.title = title
            self.message = message
            self.context = context
            self.stackTrace = stackTrace
            self.severity = severity
        }
    }
    
    // MARK: - Reporting
    
    /// Report an error
    func report(_ error: Error, context: String? = nil, severity: AppError.Severity = .error) {
        let appError = AppError(
            title: String(describing: type(of: error)),
            message: error.localizedDescription,
            context: context,
            stackTrace: shouldCaptureStackTrace(severity: severity) ? Thread.callStackSymbols.joined(separator: "\n") : nil,
            severity: severity
        )
        
        addError(appError)
        logToFile(appError)
        
        if severity == .critical {
            showCriticalAlert(appError)
        }
    }
    
    /// Report a custom error
    func report(title: String, message: String, context: String? = nil, severity: AppError.Severity = .error) {
        let appError = AppError(
            title: title,
            message: message,
            context: context,
            stackTrace: shouldCaptureStackTrace(severity: severity) ? Thread.callStackSymbols.joined(separator: "\n") : nil,
            severity: severity
        )
        
        addError(appError)
        logToFile(appError)
        
        if severity == .critical {
            showCriticalAlert(appError)
        }
    }
    
    /// Determine if stack trace should be captured based on severity
    private func shouldCaptureStackTrace(severity: AppError.Severity) -> Bool {
        // Only capture stack traces for error and critical severity to reduce overhead
        switch severity {
        case .info, .warning:
            return false
        case .error, .critical:
            return true
        }
    }
    
    private func addError(_ error: AppError) {
        DispatchQueue.main.async {
            self.recentErrors.insert(error, at: 0)
            if self.recentErrors.count > self.maxErrors {
                self.recentErrors.removeLast()
            }
            self.hasUnreadErrors = true
        }
    }
    
    // MARK: - File Logging
    
    private func logToFile(_ error: AppError) {
        let formatter = ISO8601DateFormatter()
        let entry = """
        [\(formatter.string(from: error.timestamp))] [\(error.severity.rawValue.uppercased())] \(error.title)
        Message: \(error.message)
        Context: \(error.context ?? "none")
        ---
        
        """
        
        DispatchQueue.global(qos: .utility).async { [weak self] in
            guard let self = self else { return }
            
            do {
                try OnibiConfig.ensureDirectoryExists()
                
                if FileManager.default.fileExists(atPath: self.errorLogPath) {
                    let handle = try FileHandle(forWritingTo: URL(fileURLWithPath: self.errorLogPath))
                    defer { try? handle.close() }
                    try handle.seekToEnd()
                    if let data = entry.data(using: .utf8) {
                        try handle.write(contentsOf: data)
                    }
                } else {
                    try entry.write(toFile: self.errorLogPath, atomically: true, encoding: .utf8)
                }
                
                // Rotate if too large (> 1MB)
                self.rotateLogIfNeeded()
            } catch {
                print("[ErrorReporter] Failed to write error log: \(error)")
            }
        }
    }
    
    private func rotateLogIfNeeded() {
        let fm = FileManager.default
        guard let attrs = try? fm.attributesOfItem(atPath: errorLogPath),
              let size = attrs[.size] as? Int64,
              size > 1_000_000 else { return }
        
        // Keep backup and truncate
        let backupPath = errorLogPath + ".1"
        try? fm.removeItem(atPath: backupPath)
        try? fm.moveItem(atPath: errorLogPath, toPath: backupPath)
    }
    
    // MARK: - GitHub Integration
    
    /// Generate GitHub issue URL
    func generateGitHubIssueURL(for error: AppError) -> URL? {
        let title = "[\(error.severity.rawValue.uppercased())] \(error.title)"
        
        let sysInfo = """
        **System Info:**
        - macOS: \(ProcessInfo.processInfo.operatingSystemVersionString)
        - App Version: \(Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "unknown")
        - Build: \(Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? "unknown")
        """
        
        let body = """
        ## Error Details
        
        **Title:** \(error.title)
        **Message:** \(error.message)
        **Context:** \(error.context ?? "N/A")
        **Timestamp:** \(ISO8601DateFormatter().string(from: error.timestamp))
        
        \(sysInfo)
        
        ## Steps to Reproduce
        <!-- Please describe what you were doing when this error occurred -->
        
        1. 
        2. 
        3. 
        
        ## Additional Context
        <!-- Any other relevant information -->
        """
        
        var components = URLComponents(string: "https://github.com/\(githubRepo)/issues/new")!
        components.queryItems = [
            URLQueryItem(name: "title", value: title),
            URLQueryItem(name: "body", value: body),
            URLQueryItem(name: "labels", value: "bug")
        ]
        
        return components.url
    }
    
    /// Open GitHub issue in browser
    func openGitHubIssue(for error: AppError) {
        guard let url = generateGitHubIssueURL(for: error) else { return }
        NSWorkspace.shared.open(url)
    }
    
    // MARK: - Critical Alerts
    
    /// Show NSAlert for critical errors
    func showCriticalAlert(_ error: AppError) {
        DispatchQueue.main.async {
            let alert = NSAlert()
            alert.messageText = error.title
            alert.informativeText = "\(error.message)\n\nWould you like to report this issue?"
            alert.alertStyle = .critical
            alert.addButton(withTitle: "Report on GitHub")
            alert.addButton(withTitle: "Copy Details")
            alert.addButton(withTitle: "Dismiss")
            
            let response = alert.runModal()
            
            switch response {
            case .alertFirstButtonReturn:
                self.openGitHubIssue(for: error)
            case .alertSecondButtonReturn:
                self.copyErrorDetails(error)
            default:
                break
            }
        }
    }
    
    /// Copy error details to clipboard
    func copyErrorDetails(_ error: AppError) {
        let details = """
        Error: \(error.title)
        Message: \(error.message)
        Context: \(error.context ?? "N/A")
        Timestamp: \(ISO8601DateFormatter().string(from: error.timestamp))
        Severity: \(error.severity.rawValue)
        
        Stack Trace:
        \(error.stackTrace ?? "N/A")
        """
        
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(details, forType: .string)
    }
    
    // MARK: - Health Check
    
    /// Perform health check on app launch
    func performHealthCheck() -> [String] {
        var issues: [String] = []
        
        let fm = FileManager.default
        let dataDir = OnibiConfig.appDataDirectory
        
        // Check directory exists
        if !fm.fileExists(atPath: dataDir) {
            do {
                try fm.createDirectory(atPath: dataDir, withIntermediateDirectories: true)
            } catch {
                issues.append("Cannot create data directory: \(error.localizedDescription)")
            }
        }
        
        // Check write permissions
        let testFile = dataDir + "/.write_test"
        do {
            try "test".write(toFile: testFile, atomically: true, encoding: .utf8)
            try fm.removeItem(atPath: testFile)
        } catch {
            issues.append("No write permission for data directory: \(error.localizedDescription)")
        }
        
        // Check JSON logs validity
        let logsPath = dataDir + "/logs.json"
        if fm.fileExists(atPath: logsPath) {
            if let data = fm.contents(atPath: logsPath) {
                do {
                    _ = try JSONSerialization.jsonObject(with: data)
                } catch {
                    issues.append("logs.json is corrupted: \(error.localizedDescription)")
                }
            }
        }
        
        return issues
    }
    
    // MARK: - Persistence
    
    private func loadRecentErrors() {
        // Load from file on startup (last 10 only for memory)
        guard FileManager.default.fileExists(atPath: errorLogPath),
              let content = try? String(contentsOfFile: errorLogPath, encoding: .utf8) else { return }
        
        // Parse last entries (simplified)
        let lines = content.components(separatedBy: "---\n").suffix(10)
        // Note: Full parsing would require more complex logic
    }
    
    /// Clear all errors
    func clearErrors() {
        recentErrors.removeAll()
        hasUnreadErrors = false
    }
    
    /// Mark errors as read
    func markAsRead() {
        hasUnreadErrors = false
    }
}
