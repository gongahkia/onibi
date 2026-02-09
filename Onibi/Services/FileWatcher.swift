import Foundation
import Combine

/// Monitors file system changes using FSEvents API
final class FileWatcher: ObservableObject {
    private var stream: FSEventStreamRef?
    private let path: String
    private let callback: () -> Void
    private var lastEventId: FSEventStreamEventId = FSEventStreamEventId(kFSEventStreamEventIdSinceNow)
    
    /// Debounce publisher to avoid excessive callbacks
    private var debounceSubject = PassthroughSubject<Void, Never>()
    private var cancellables = Set<AnyCancellable>()
    
    init(path: String, debounceInterval: TimeInterval = 0.5, callback: @escaping () -> Void) {
        self.path = path
        self.callback = callback
        
        // Set up debouncing
        debounceSubject
            .debounce(for: .seconds(debounceInterval), scheduler: DispatchQueue.main)
            .sink { [weak self] in
                self?.callback()
            }
            .store(in: &cancellables)
    }
    
    deinit {
        stop()
    }
    
    /// Start watching the file/directory
    func start() {
        guard stream == nil else { return }
        
        var context = FSEventStreamContext(
            version: 0,
            info: Unmanaged.passRetained(self).toOpaque(),
            retain: nil,
            release: fileWatcherReleaseCallback,
            copyDescription: nil
        )
        
        let pathsToWatch = [path] as CFArray
        
        stream = FSEventStreamCreate(
            nil,
            { (_, info, numEvents, eventPaths, eventFlags, eventIds) in
                guard let info = info else { return }
                let watcher = Unmanaged<FileWatcher>.fromOpaque(info).takeUnretainedValue()
                watcher.handleEvents(numEvents: numEvents, eventPaths: eventPaths, eventFlags: eventFlags)
            },
            &context,
            pathsToWatch,
            lastEventId,
            0.5, // Latency in seconds
            FSEventStreamCreateFlags(kFSEventStreamCreateFlagFileEvents | kFSEventStreamCreateFlagUseCFTypes)
        )
        
        if let stream = stream {
            self.stream = stream
        } else {
            // Stream creation failed, release the retained self
            if let info = context.info {
                Unmanaged<FileWatcher>.fromOpaque(info).release()
            }
            return
        }
        
        FSEventStreamScheduleWithRunLoop(stream, CFRunLoopGetMain(), CFRunLoopMode.defaultMode.rawValue)
        FSEventStreamStart(stream)
    }
    
    /// Stop watching
    func stop() {
        guard let stream = stream else { return }
        FSEventStreamStop(stream)
        FSEventStreamInvalidate(stream)
        FSEventStreamRelease(stream)
        self.stream = nil
    }
    
    private func handleEvents(numEvents: Int, eventPaths: UnsafeMutableRawPointer, eventFlags: UnsafePointer<FSEventStreamEventFlags>) {
        // Trigger debounced callback
        debounceSubject.send()
    }

}

private func fileWatcherReleaseCallback(info: UnsafeRawPointer?) {
    guard let info = info else { return }
    Unmanaged<FileWatcher>.fromOpaque(info).release()
}

/// Ghostty integration configuration
struct OnibiConfig {
    /// Default locations to check for Ghostty config
    static let configLocations: [String] = [
        NSHomeDirectory() + "/.config/ghostty/config",
        NSHomeDirectory() + "/Library/Application Support/com.mitchellh.ghostty/config"
    ]
    
    /// App's log file location (shell hooks write here)
    static let logFilePath: String = {
        let configDir = NSHomeDirectory() + "/.config/onibi"
        return configDir + "/terminal.log"
    }()
    
    /// Directory for app data
    static let appDataDirectory: String = {
        return NSHomeDirectory() + "/.config/onibi"
    }()
    
    /// Ensure app data directory exists
    static func ensureDirectoryExists() throws {
        let fm = FileManager.default
        if !fm.fileExists(atPath: appDataDirectory) {
            try fm.createDirectory(atPath: appDataDirectory, withIntermediateDirectories: true)
        }
    }
    
    /// Shell hook script for zsh to log commands
    static let zshHookScript: String = """
    # Onibi Integration
    # Add to ~/.zshrc
    
    _onibi_preexec() {
        echo "$(date -Iseconds)|CMD_START|$1" >> ~/.config/onibi/terminal.log
    }
    
    _onibi_precmd() {
        local exit_code=$?
        echo "$(date -Iseconds)|CMD_END|$exit_code" >> ~/.config/onibi/terminal.log
    }
    
    autoload -Uz add-zsh-hook
    add-zsh-hook preexec _onibi_preexec
    add-zsh-hook precmd _onibi_precmd
    """
    
    /// Shell hook script for bash
    static let bashHookScript: String = """
    # Onibi Integration
    # Add to ~/.bashrc
    
    _onibi_preexec() {
        echo "$(date -Iseconds)|CMD_START|$BASH_COMMAND" >> ~/.config/onibi/terminal.log
    }
    
    trap '_onibi_preexec' DEBUG
    
    PROMPT_COMMAND='echo "$(date -Iseconds)|CMD_END|$?" >> ~/.config/onibi/terminal.log'
    """
}
