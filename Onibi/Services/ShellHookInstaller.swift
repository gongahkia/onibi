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

        var defaultShellPath: String {
            switch self {
            case .zsh: return "/bin/zsh"
            case .bash: return "/bin/bash"
            case .fish: return "/opt/homebrew/bin/fish"
            }
        }

        func hookScript(
            logPath: String,
            remoteControlEnabled: Bool = false,
            proxyBinaryPath: String? = nil,
            proxySocketPath: String? = nil,
            proxyVersion: String? = nil,
            shellArguments: [String] = []
        ) -> String {
            switch self {
            case .zsh:
                return Self.zshHookScript(
                    logPath: logPath,
                    remoteControlEnabled: remoteControlEnabled,
                    proxyBinaryPath: proxyBinaryPath,
                    proxySocketPath: proxySocketPath,
                    proxyVersion: proxyVersion,
                    shellArguments: shellArguments
                )
            case .bash:
                return Self.bashHookScript(
                    logPath: logPath,
                    remoteControlEnabled: remoteControlEnabled,
                    proxyBinaryPath: proxyBinaryPath,
                    proxySocketPath: proxySocketPath,
                    proxyVersion: proxyVersion,
                    shellArguments: shellArguments
                )
            case .fish:
                return Self.fishHookScript(
                    logPath: logPath,
                    remoteControlEnabled: remoteControlEnabled,
                    proxyBinaryPath: proxyBinaryPath,
                    proxySocketPath: proxySocketPath,
                    proxyVersion: proxyVersion,
                    shellArguments: shellArguments
                )
            }
        }
        
        // Marker comments for identification
        static let startMarker = "# >>> onibi >>>"
        static let endMarker = "# <<< onibi <<<"
        
        // Shell-specific scripts
        static func zshHookScript(logPath: String) -> String {
            zshHookScript(
                logPath: logPath,
                remoteControlEnabled: false,
                proxyBinaryPath: nil,
                proxySocketPath: nil,
                proxyVersion: nil,
                shellArguments: []
            )
        }

        static func zshHookScript(
            logPath: String,
            remoteControlEnabled: Bool,
            proxyBinaryPath: String?,
            proxySocketPath: String?,
            proxyVersion: String?,
            shellArguments: [String]
        ) -> String {
            let bootstrapBlock = zshProxyBootstrapBlock(
                remoteControlEnabled: remoteControlEnabled,
                proxyBinaryPath: proxyBinaryPath,
                proxySocketPath: proxySocketPath,
                proxyVersion: proxyVersion,
                shellArguments: shellArguments
            )

            return """
            # >>> onibi >>>
            # Onibi Shell Integration - DO NOT EDIT
            \(bootstrapBlock)
            _onibi_b64() {
                printf "%s" "$1" | base64 | tr -d '\\n'
            }

            _onibi_emit_command_start() {
                [[ "$ONIBI_SESSION_PROXY_ACTIVE" == "1" ]] || return 0
                local command_b64 cwd_b64
                command_b64="$(_onibi_b64 "$1")"
                cwd_b64="$(_onibi_b64 "$PWD")"
                printf '\\033]1337;OnibiCommandStart;command=%s;cwd=%s\\a' "$command_b64" "$cwd_b64"
            }

            _onibi_emit_command_end() {
                [[ "$ONIBI_SESSION_PROXY_ACTIVE" == "1" ]] || return 0
                local cwd_b64
                cwd_b64="$(_onibi_b64 "$PWD")"
                printf '\\033]1337;OnibiCommandEnd;exit=%s;cwd=%s\\a' "$1" "$cwd_b64"
            }

            _onibi_preexec() {
                local session_id="${TERM_SESSION_ID:-$$}"
                _onibi_emit_command_start "$1"
                echo "$(date -Iseconds)|CMD_START|$session_id|$1" >> \(logPath)
            }
            
            _onibi_precmd() {
                local exit_code=$?
                local session_id="${TERM_SESSION_ID:-$$}"
                _onibi_emit_command_end "$exit_code"
                echo "$(date -Iseconds)|CMD_END|$session_id|$exit_code" >> \(logPath)
            }
            
            autoload -Uz add-zsh-hook
            add-zsh-hook preexec _onibi_preexec
            add-zsh-hook precmd _onibi_precmd
            # <<< onibi <<<
            """
        }

        static func bashHookScript(logPath: String) -> String {
            bashHookScript(
                logPath: logPath,
                remoteControlEnabled: false,
                proxyBinaryPath: nil,
                proxySocketPath: nil,
                proxyVersion: nil,
                shellArguments: []
            )
        }

        static func bashHookScript(
            logPath: String,
            remoteControlEnabled: Bool,
            proxyBinaryPath: String?,
            proxySocketPath: String?,
            proxyVersion: String?,
            shellArguments: [String]
        ) -> String {
            let bootstrapBlock = bashProxyBootstrapBlock(
                remoteControlEnabled: remoteControlEnabled,
                proxyBinaryPath: proxyBinaryPath,
                proxySocketPath: proxySocketPath,
                proxyVersion: proxyVersion,
                shellArguments: shellArguments
            )

            return """
            # >>> onibi >>>
            # Onibi Shell Integration - DO NOT EDIT
            \(bootstrapBlock)
            _onibi_b64() {
                printf "%s" "$1" | base64 | tr -d '\\n'
            }

            _onibi_emit_command_start() {
                [[ "$ONIBI_SESSION_PROXY_ACTIVE" == "1" ]] || return 0
                local command_b64 cwd_b64
                command_b64="$(_onibi_b64 "$1")"
                cwd_b64="$(_onibi_b64 "$PWD")"
                printf '\\033]1337;OnibiCommandStart;command=%s;cwd=%s\\a' "$command_b64" "$cwd_b64"
            }

            _onibi_emit_command_end() {
                [[ "$ONIBI_SESSION_PROXY_ACTIVE" == "1" ]] || return 0
                local cwd_b64
                cwd_b64="$(_onibi_b64 "$PWD")"
                printf '\\033]1337;OnibiCommandEnd;exit=%s;cwd=%s\\a' "$1" "$cwd_b64"
            }

            _onibi_preexec() {
                local session_id="${TERM_SESSION_ID:-$$}"
                _onibi_emit_command_start "$BASH_COMMAND"
                echo "$(date -Iseconds)|CMD_START|$session_id|$BASH_COMMAND" >> \(logPath)
            }
            
            trap '_onibi_preexec' DEBUG
            
            # Check for existing PROMPT_COMMAND and preserve it
            _onibi_prompt_cmd() {
                local _onibi_exit=$?
                _onibi_emit_command_end "$_onibi_exit"
                echo "$(date -Iseconds)|CMD_END|${TERM_SESSION_ID:-$$}|$_onibi_exit" >> \(logPath)
                return $_onibi_exit
            }
            if [[ -n "$PROMPT_COMMAND" && "$PROMPT_COMMAND" != *"_onibi_prompt_cmd"* ]]; then
                PROMPT_COMMAND="_onibi_prompt_cmd; $PROMPT_COMMAND"
            elif [[ -z "$PROMPT_COMMAND" ]]; then
                PROMPT_COMMAND="_onibi_prompt_cmd"
            fi
            # <<< onibi <<<
            """
        }

        static func fishHookScript(logPath: String) -> String {
            fishHookScript(
                logPath: logPath,
                remoteControlEnabled: false,
                proxyBinaryPath: nil,
                proxySocketPath: nil,
                proxyVersion: nil,
                shellArguments: []
            )
        }

        static func fishHookScript(
            logPath: String,
            remoteControlEnabled: Bool,
            proxyBinaryPath: String?,
            proxySocketPath: String?,
            proxyVersion: String?,
            shellArguments: [String]
        ) -> String {
            let bootstrapBlock = fishProxyBootstrapBlock(
                remoteControlEnabled: remoteControlEnabled,
                proxyBinaryPath: proxyBinaryPath,
                proxySocketPath: proxySocketPath,
                proxyVersion: proxyVersion,
                shellArguments: shellArguments
            )

            return """
            # >>> onibi >>>
            # Onibi Shell Integration - DO NOT EDIT
            \(bootstrapBlock)
            function _onibi_b64
                printf "%s" "$argv[1]" | base64 | tr -d '\\n'
            end

            function _onibi_emit_command_start
                test "$ONIBI_SESSION_PROXY_ACTIVE" = "1"; or return 0
                set -l command_b64 (_onibi_b64 "$argv[1]")
                set -l cwd_b64 (_onibi_b64 "$PWD")
                printf '\\033]1337;OnibiCommandStart;command=%s;cwd=%s\\a' "$command_b64" "$cwd_b64"
            end

            function _onibi_emit_command_end
                test "$ONIBI_SESSION_PROXY_ACTIVE" = "1"; or return 0
                set -l cwd_b64 (_onibi_b64 "$PWD")
                printf '\\033]1337;OnibiCommandEnd;exit=%s;cwd=%s\\a' "$argv[1]" "$cwd_b64"
            end

            function _onibi_preexec --on-event fish_preexec
                set -l session_id (echo $TERM_SESSION_ID; or echo %self)
                set -l command (string join " " -- $argv)
                _onibi_emit_command_start "$command"
                echo (date -Iseconds)"|CMD_START|$session_id|$argv" >> \(logPath)
            end
            
            function _onibi_postexec --on-event fish_postexec
                set -l session_id (echo $TERM_SESSION_ID; or echo %self)
                _onibi_emit_command_end "$status"
                echo (date -Iseconds)"|CMD_END|$session_id|$status" >> \(logPath)
            end
            # <<< onibi <<<
            """
        }

        private static func zshProxyBootstrapBlock(
            remoteControlEnabled: Bool,
            proxyBinaryPath: String?,
            proxySocketPath: String?,
            proxyVersion: String?,
            shellArguments: [String]
        ) -> String {
            guard
                remoteControlEnabled,
                let proxyBinaryPath,
                let proxySocketPath,
                let proxyVersion
            else {
                return ""
            }

            let escapedBinary = shellDoubleQuoted(proxyBinaryPath)
            let escapedSocket = shellDoubleQuoted(proxySocketPath)
            let escapedVersion = shellDoubleQuoted(proxyVersion)
            let escapedArgs = shellDoubleQuoted(shellArguments.joined(separator: " "))

            return """
            if [[ -o interactive ]] && [[ -z "$ONIBI_SESSION_PROXY_ACTIVE" ]] && ([[ "$TERM_PROGRAM" == "ghostty" ]] || [[ -n "$GHOSTTY_RESOURCES_DIR" ]]) && [[ -x \(escapedBinary) ]]; then
                export ONIBI_SESSION_PROXY_ACTIVE=1
                export ONIBI_PROXY_SOCKET_PATH=\(escapedSocket)
                export ONIBI_HOST_SESSION_ID="${TERM_SESSION_ID:-$(uuidgen 2>/dev/null || echo $$)}"
                export ONIBI_PARENT_SHELL="${SHELL:-/bin/zsh}"
                export ONIBI_PARENT_SHELL_ARGS=\(escapedArgs)
                export ONIBI_PROXY_VERSION=\(escapedVersion)
                exec \(escapedBinary)
            fi

            """
        }

        private static func bashProxyBootstrapBlock(
            remoteControlEnabled: Bool,
            proxyBinaryPath: String?,
            proxySocketPath: String?,
            proxyVersion: String?,
            shellArguments: [String]
        ) -> String {
            guard
                remoteControlEnabled,
                let proxyBinaryPath,
                let proxySocketPath,
                let proxyVersion
            else {
                return ""
            }

            let escapedBinary = shellDoubleQuoted(proxyBinaryPath)
            let escapedSocket = shellDoubleQuoted(proxySocketPath)
            let escapedVersion = shellDoubleQuoted(proxyVersion)
            let escapedArgs = shellDoubleQuoted(shellArguments.joined(separator: " "))

            return """
            case $- in
                *i*)
                    if [[ -z "$ONIBI_SESSION_PROXY_ACTIVE" ]] && ([[ "$TERM_PROGRAM" == "ghostty" ]] || [[ -n "$GHOSTTY_RESOURCES_DIR" ]]) && [[ -x \(escapedBinary) ]]; then
                        export ONIBI_SESSION_PROXY_ACTIVE=1
                        export ONIBI_PROXY_SOCKET_PATH=\(escapedSocket)
                        export ONIBI_HOST_SESSION_ID="${TERM_SESSION_ID:-$(uuidgen 2>/dev/null || echo $$)}"
                        export ONIBI_PARENT_SHELL="${SHELL:-/bin/bash}"
                        export ONIBI_PARENT_SHELL_ARGS=\(escapedArgs)
                        export ONIBI_PROXY_VERSION=\(escapedVersion)
                        exec \(escapedBinary)
                    fi
                    ;;
            esac

            """
        }

        private static func fishProxyBootstrapBlock(
            remoteControlEnabled: Bool,
            proxyBinaryPath: String?,
            proxySocketPath: String?,
            proxyVersion: String?,
            shellArguments: [String]
        ) -> String {
            guard
                remoteControlEnabled,
                let proxyBinaryPath,
                let proxySocketPath,
                let proxyVersion
            else {
                return ""
            }

            let escapedBinary = shellDoubleQuoted(proxyBinaryPath)
            let escapedSocket = shellDoubleQuoted(proxySocketPath)
            let escapedVersion = shellDoubleQuoted(proxyVersion)
            let escapedArgs = shellDoubleQuoted(shellArguments.joined(separator: " "))

            return """
            if status is-interactive; and test -z "$ONIBI_SESSION_PROXY_ACTIVE"; and begin; test "$TERM_PROGRAM" = "ghostty"; or test -n "$GHOSTTY_RESOURCES_DIR"; end; and test -x \(escapedBinary)
                set -gx ONIBI_SESSION_PROXY_ACTIVE 1
                set -gx ONIBI_PROXY_SOCKET_PATH \(escapedSocket)
                if set -q TERM_SESSION_ID
                    set -gx ONIBI_HOST_SESSION_ID "$TERM_SESSION_ID"
                else
                    set -gx ONIBI_HOST_SESSION_ID (uuidgen ^/dev/null; or echo %self)
                end
                if set -q SHELL
                    set -gx ONIBI_PARENT_SHELL "$SHELL"
                else
                    set -gx ONIBI_PARENT_SHELL "/opt/homebrew/bin/fish"
                end
                set -gx ONIBI_PARENT_SHELL_ARGS \(escapedArgs)
                set -gx ONIBI_PROXY_VERSION \(escapedVersion)
                exec \(escapedBinary)
            end

            """
        }

        private static func shellDoubleQuoted(_ value: String) -> String {
            let escaped = value
                .replacingOccurrences(of: "\\", with: "\\\\")
                .replacingOccurrences(of: "\"", with: "\\\"")
                .replacingOccurrences(of: "$", with: "\\$")
                .replacingOccurrences(of: "`", with: "\\`")
            return "\"\(escaped)\""
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
            if containsExactMarkerLine(contents, marker: Shell.startMarker) &&
               containsExactMarkerLine(contents, marker: Shell.endMarker) {
                return .installed
            }
            return .notInstalled
        } catch {
            DiagnosticsStore.shared.record(
                component: "ShellHookInstaller",
                level: .warning,
                message: "failed reading shell rc file status",
                metadata: [
                    "shell": shell.rawValue,
                    "path": shell.rcFilePath,
                    "reason": error.localizedDescription
                ]
            )
            return .error("Cannot read \(shell.rcFilePath)")
        }
    }
    
    /// Check for exact marker line to avoid partial matches
    private func containsExactMarkerLine(_ contents: String, marker: String) -> Bool {
        let lines = contents.components(separatedBy: .newlines)
        return lines.contains { $0.trimmingCharacters(in: .whitespaces) == marker }
    }
    
    // MARK: - Installation

    /// Install hooks for a shell, replacing any existing Onibi block.
    func installOrUpdate(for shell: Shell) throws {
        if case .installed = checkStatus(for: shell) {
            try uninstall(from: shell)
        }
        try install(for: shell)
    }
    
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
        
        // Check if already installed (use exact line match)
        if containsExactMarkerLine(existingContent, marker: Shell.startMarker) {
            throw InstallError.alreadyInstalled
        }
        
        // Append hook script
        let settings = SettingsViewModel.shared.settings
        let newContent = existingContent + "\n\n" + SessionProxyCoordinator.shared.hookScript(for: shell, settings: settings) + "\n"
        try newContent.write(toFile: shell.rcFilePath, atomically: true, encoding: .utf8)
        
        // Update status
        DispatchQueue.main.async {
            self.shellStatuses[shell] = .installed
        }
    }
    
    /// Create backup of rc file with rotation (keep last 3)
    func createBackup(for shell: Shell) throws {
        guard rcFileExists(for: shell) else { return }
        
        let fm = FileManager.default
        let basePath = shell.rcFilePath + ".onibi-backup"
        
        // Rotate backups: .3 -> delete, .2 -> .3, .1 -> .2, current -> .1
        let backup3 = basePath + ".3"
        let backup2 = basePath + ".2"
        let backup1 = basePath + ".1"
        
        // Delete oldest backup if exists
        if fm.fileExists(atPath: backup3) {
            try fm.removeItem(atPath: backup3)
        }
        
        // Rotate .2 -> .3
        if fm.fileExists(atPath: backup2) {
            try fm.moveItem(atPath: backup2, toPath: backup3)
        }
        
        // Rotate .1 -> .2
        if fm.fileExists(atPath: backup1) {
            try fm.moveItem(atPath: backup1, toPath: backup2)
        }
        
        // Rotate current backup -> .1
        if fm.fileExists(atPath: basePath) {
            try fm.moveItem(atPath: basePath, toPath: backup1)
        }
        
        // Create new backup
        try fm.copyItem(atPath: shell.rcFilePath, toPath: basePath)
    }
    
    // MARK: - Uninstallation
    
    /// Remove hooks from a shell
    func uninstall(from shell: Shell) throws {
        guard rcFileExists(for: shell) else { return }
        
        let contents = try String(contentsOfFile: shell.rcFilePath, encoding: .utf8)
        
        // Find and remove the hook section line by line to preserve file formatting
        var lines = contents.components(separatedBy: "\n")
        var inHookSection = false
        var indicesToRemove: [Int] = []
        
        for (index, line) in lines.enumerated() {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed == Shell.startMarker {
                inHookSection = true
            }
            if inHookSection {
                indicesToRemove.append(index)
            }
            if trimmed == Shell.endMarker {
                inHookSection = false
            }
        }
        
        // Remove from end to start to keep indices valid
        for index in indicesToRemove.reversed() {
            lines.remove(at: index)
        }
        
        // Remove at most 2 blank lines that may precede the removed section
        // (since we typically add \n\n before the hook script)
        var blankLinesRemoved = 0
        if let firstRemovedIndex = indicesToRemove.first {
            var checkIndex = min(firstRemovedIndex, lines.count)
            while checkIndex > 0 && blankLinesRemoved < 2 {
                let prevIndex = checkIndex - 1
                if prevIndex < lines.count && lines[prevIndex].trimmingCharacters(in: .whitespaces).isEmpty {
                    lines.remove(at: prevIndex)
                    blankLinesRemoved += 1
                    checkIndex -= 1
                } else {
                    break
                }
            }
        }
        
        let newContents = lines.joined(separator: "\n")
        try newContents.write(toFile: shell.rcFilePath, atomically: true, encoding: .utf8)
        
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
        let testMarker = "TEST|verification"
        let testEntry = "\(ISO8601DateFormatter().string(from: Date()))|\(testMarker)\n"
        
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
            let verified = contents.contains(testMarker)
            
            // Clean up test entry
            if verified {
                cleanupTestEntry(at: testPath, marker: testMarker)
            }
            
            return verified
        } catch {
            // File might not exist yet, try creating
            do {
                try OnibiConfig.ensureDirectoryExists()
                try testEntry.write(toFile: testPath, atomically: true, encoding: .utf8)
                // Clean up test entry
                cleanupTestEntry(at: testPath, marker: testMarker)
                return true
            } catch {
                DiagnosticsStore.shared.record(
                    component: "ShellHookInstaller",
                    level: .warning,
                    message: "hook verification failed while creating fallback test file",
                    metadata: [
                        "path": testPath,
                        "reason": error.localizedDescription
                    ]
                )
                return false
            }
        }
    }
    
    /// Remove test verification entries from log file
    private func cleanupTestEntry(at path: String, marker: String) {
        do {
            let contents = try String(contentsOfFile: path, encoding: .utf8)
            let lines = contents.components(separatedBy: "\n")
            let filteredLines = lines.filter { !$0.contains(marker) }
            let cleanedContents = filteredLines.joined(separator: "\n")
            try cleanedContents.write(toFile: path, atomically: true, encoding: .utf8)
        } catch {
            DiagnosticsStore.shared.record(
                component: "ShellHookInstaller",
                level: .warning,
                message: "failed cleaning temporary hook verification entry",
                metadata: [
                    "path": path,
                    "reason": error.localizedDescription
                ]
            )
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
