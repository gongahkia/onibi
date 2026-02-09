import Foundation

/// Service for managing shell hook installation
final class ShellHookInstaller: ObservableObject {
    static let shared = ShellHookInstaller()
    
    // MARK: - Shell Types
    
    enum Shell: String, CaseIterable {
        case zsh = "zsh"
        case bash = "bash"
        case fish = "fish"
        
        var rcFilePath: String {
            switch self {
            case .zsh: return NSHomeDirectory() + "/.zshrc"
            case .bash: return NSHomeDirectory() + "/.bashrc"
            case .fish: return NSHomeDirectory() + "/.config/fish/config.fish"
            }
        }
        
        func hookScript(logPath: String) -> String {
            switch self {
            case .zsh:
                return Self.zshHookScript(logPath: logPath)
            case .bash:
                return Self.bashHookScript(logPath: logPath)
            case .fish:
                return Self.fishHookScript(logPath: logPath)
            }
        }
        
        // Marker comments for identification
        static let startMarker = "# >>> onibi >>>"
        static let endMarker = "# <<< onibi <<<"
        
        // Shell-specific scripts
        static func zshHookScript(logPath: String) -> String {
            """
            # >>> onibi >>>
            # Onibi Shell Integration - DO NOT EDIT
            _onibi_preexec() {
                local session_id="${TERM_SESSION_ID:-$$}"
                echo "$(date -Iseconds)|CMD_START|$session_id|$1" >> \(logPath)
            }
            
            _onibi_precmd() {
                local exit_code=$?
                local session_id="${TERM_SESSION_ID:-$$}"
                echo "$(date -Iseconds)|CMD_END|$session_id|$exit_code" >> \(logPath)
            }
            
            autoload -Uz add-zsh-hook
            add-zsh-hook preexec _onibi_preexec
            add-zsh-hook precmd _onibi_precmd
            # <<< onibi <<<
            """
        }
        
        static func bashHookScript(logPath: String) -> String {
            """
            # >>> onibi >>>
            # Onibi Shell Integration - DO NOT EDIT
            _onibi_preexec() {
                local session_id="${TERM_SESSION_ID:-$$}"
                echo "$(date -Iseconds)|CMD_START|$session_id|$BASH_COMMAND" >> \(logPath)
            }
            
            trap '_onibi_preexec' DEBUG
            
            PROMPT_COMMAND='_onibi_exit=$?; echo "$(date -Iseconds)|CMD_END|${TERM_SESSION_ID:-$$}|$_onibi_exit" >> \(logPath); '$PROMPT_COMMAND
            # <<< onibi <<<
            """
        }
        
        static func fishHookScript(logPath: String) -> String {
            """
            # >>> onibi >>>
            # Onibi Shell Integration - DO NOT EDIT
            function _onibi_preexec --on-event fish_preexec
                set -l session_id (echo $TERM_SESSION_ID; or echo %self)
                echo (date -Iseconds)"|CMD_START|$session_id|$argv" >> \(logPath)
            end
            
            function _onibi_postexec --on-event fish_postexec
                set -l session_id (echo $TERM_SESSION_ID; or echo %self)
                echo (date -Iseconds)"|CMD_END|$session_id|$status" >> \(logPath)
            end
            # <<< onibi <<<
            """
        }
    }
    
    // MARK: - Status
    
    enum InstallationStatus: Equatable {
        case notInstalled
        case installed
        case error(String)
    }
    
    @Published var shellStatuses: [Shell: InstallationStatus] = [:]
    @Published var detectedShell: Shell?
    
    private init() {
        detectCurrentShell()
        checkAllShellStatuses()
    }
    
    // MARK: - Shell Detection
    
    /// Detect the user's default shell
    func detectCurrentShell() {
        if let shellPath = ProcessInfo.processInfo.environment["SHELL"] {
            let shellName = (shellPath as NSString).lastPathComponent
            detectedShell = Shell(rawValue: shellName)
        }
    }
    
    /// Check if rc file exists for a shell
    func rcFileExists(for shell: Shell) -> Bool {
        FileManager.default.fileExists(atPath: shell.rcFilePath)
    }
    
    // MARK: - Status Checking
    
    /// Check installation status for all shells
    func checkAllShellStatuses() {
        for shell in Shell.allCases {
            shellStatuses[shell] = checkStatus(for: shell)
        }
    }
    
    /// Check if hooks are installed for a specific shell
    func checkStatus(for shell: Shell) -> InstallationStatus {
        guard rcFileExists(for: shell) else {
            return .notInstalled
        }
        
        do {
            let contents = try String(contentsOfFile: shell.rcFilePath, encoding: .utf8)
            if contents.contains(Shell.startMarker) && contents.contains(Shell.endMarker) {
                return .installed
            }
            return .notInstalled
        } catch {
            return .error("Cannot read \(shell.rcFilePath)")
        }
    }
    
    // MARK: - Installation
    
    /// Install hooks for a shell with backup
    func install(for shell: Shell) throws {
        // Create backup first
        try createBackup(for: shell)
        
        // Ensure config directory exists
        try OnibiConfig.ensureDirectoryExists()
        
        // Read existing content or create new file
        var existingContent = ""
        if rcFileExists(for: shell) {
            existingContent = try String(contentsOfFile: shell.rcFilePath, encoding: .utf8)
        }
        
        // Check if already installed
        if existingContent.contains(Shell.startMarker) {
            throw InstallError.alreadyInstalled
        }
        
        // Append hook script
        let settings = SettingsViewModel.shared.settings
        let newContent = existingContent + "\n\n" + shell.hookScript(logPath: settings.logFilePath) + "\n"
        try newContent.write(toFile: shell.rcFilePath, atomically: true, encoding: .utf8)
        
        // Update status
        DispatchQueue.main.async {
            self.shellStatuses[shell] = .installed
        }
    }
    
    /// Create backup of rc file
    func createBackup(for shell: Shell) throws {
        guard rcFileExists(for: shell) else { return }
        
        let backupPath = shell.rcFilePath + ".onibi-backup"
        let fm = FileManager.default
        
        // Remove old backup if exists
        if fm.fileExists(atPath: backupPath) {
            try fm.removeItem(atPath: backupPath)
        }
        
        try fm.copyItem(atPath: shell.rcFilePath, toPath: backupPath)
    }
    
    // MARK: - Uninstallation
    
    /// Remove hooks from a shell
    func uninstall(from shell: Shell) throws {
        guard rcFileExists(for: shell) else { return }
        
        var contents = try String(contentsOfFile: shell.rcFilePath, encoding: .utf8)
        
        // Find and remove the hook section
        if let startRange = contents.range(of: Shell.startMarker),
           let endRange = contents.range(of: Shell.endMarker) {
            // Extend to include newlines
            let fullStart = contents.lineRange(for: startRange).lowerBound
            var fullEnd = contents.lineRange(for: endRange).upperBound
            
            // Remove trailing newlines
            while fullEnd < contents.endIndex && contents[fullEnd].isNewline {
                fullEnd = contents.index(after: fullEnd)
            }
            
            contents.removeSubrange(fullStart..<fullEnd)
            try contents.write(toFile: shell.rcFilePath, atomically: true, encoding: .utf8)
        }
        
        // Update status
        DispatchQueue.main.async {
            self.shellStatuses[shell] = .notInstalled
        }
    }
    
    // MARK: - Verification
    
    /// Test if hooks are working by writing a test entry
    func verify() -> Bool {
        let settings = SettingsViewModel.shared.settings
        let testPath = settings.logFilePath
        let testEntry = "\(ISO8601DateFormatter().string(from: Date()))|TEST|verification\n"
        
        do {
            // Write test entry
            if let data = testEntry.data(using: .utf8) {
                let handle = try FileHandle(forWritingTo: URL(fileURLWithPath: testPath))
                try handle.seekToEnd()
                try handle.write(contentsOf: data)
                try handle.close()
            }
            
            // Read back and verify
            let contents = try String(contentsOfFile: testPath, encoding: .utf8)
            return contents.contains("TEST|verification")
        } catch {
            // File might not exist yet, try creating
            do {
                try OnibiConfig.ensureDirectoryExists()
                try testEntry.write(toFile: testPath, atomically: true, encoding: .utf8)
                return true
            } catch {
                return false
            }
        }
    }
    
    // MARK: - Errors
    
    enum InstallError: LocalizedError {
        case alreadyInstalled
        case backupFailed
        case writeFailed
        
        var errorDescription: String? {
            switch self {
            case .alreadyInstalled: return "Shell hooks are already installed"
            case .backupFailed: return "Failed to create backup of shell config"
            case .writeFailed: return "Failed to write to shell config file"
            }
        }
    }
}
