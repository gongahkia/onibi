import Foundation
import Combine

/// Coordinates background log parsing and event processing
final class BackgroundTaskScheduler: ObservableObject {
    static let shared = BackgroundTaskScheduler()
    
    private var fileWatcher: FileWatcher?
    private var logBuffer: LogBuffer?
    private var logParser: LogFileParser
    private var storageManager: JSONStorageManager
    private var settings: AppSettings = .default
    
    private let eventBus = EventBus.shared
    private var cancellables = Set<AnyCancellable>()
    
    // Detectors
    private let aiDetector = AIResponseDetector()
    private let taskDetector = TaskCompletionDetector()
    private let devWorkflowParser = DevWorkflowParser()
    private let automationParser = AutomationOutputParser()
    
    // Throttling
    private var notificationThrottle: [NotificationType: Date] = [:]
    
    // LRU Cache
    private var parsedCache = LRUCache<String, GhosttyEvent>(capacity: 1000)
    
    @Published var isRunning = false
    @Published var lastParseTime: Date?
    @Published var eventsProcessed: Int = 0
    private let eventsLock = NSLock()
    
    private init() {
        self.logParser = LogFileParser()
        self.storageManager = JSONStorageManager()
        setupSubscriptions()
    }
    
    /// Start background monitoring
    func start() {
        guard !isRunning else { return }
        isRunning = true
        
        setupLogBuffer()
        setupFileWatcher()
        
        print("[BackgroundTaskScheduler] Started monitoring with profile: \(settings.logVolumeProfile.displayName)")
    }
    
    /// Stop background monitoring
    func stop() {
        isRunning = false
        fileWatcher?.stop()
        fileWatcher = nil
        
        // Final flush
        Task {
            try? await storageManager.flushToDisk()
        }
        
        print("[BackgroundTaskScheduler] Stopped")
    }
    
    /// Force a manual log refresh
    func forceRefresh() {
        processNewLogContent()
    }
    
    // MARK: - Private
    
    private func setupLogBuffer() {
        logBuffer = LogBuffer(filePath: settings.logFilePath)
        // Skip existing content on startup
        try? logBuffer?.seekToEnd()
    }
    
    private func setupFileWatcher() {
        // Watch the log directory for changes
        let logDir = URL(fileURLWithPath: settings.logFilePath).deletingLastPathComponent().path
        fileWatcher = FileWatcher(path: logDir, debounceInterval: settings.logVolumeProfile.debounceInterval) { [weak self] in
            self?.processNewLogContent()
        }
        fileWatcher?.start()
    }
    
    private func setupSubscriptions() {
        EventBus.shared.settingsPublisher
            .sink { [weak self] newSettings in
                guard let self = self else { return }
                
                let restartRequired = newSettings.logFilePath != self.settings.logFilePath ||
                                    newSettings.logVolumeProfile != self.settings.logVolumeProfile
                
                self.settings = newSettings
                
                if restartRequired {
                    self.restartMonitoring()
                }
            }
            .store(in: &cancellables)
    }
    
    private func restartMonitoring() {
        guard isRunning else { return }
        stop()
        start()
    }
    
    private func processNewLogContent() {
        guard let logBuffer = logBuffer else { return }
        
        DispatchQueue.global(qos: .utility).async { [weak self] in
            guard let self = self else { return }
            
            do {
                let newContent = try logBuffer.readNewContent()
                guard !newContent.isEmpty else { return }
                
                let lines = logBuffer.getCompleteLines(from: newContent)
                
                for line in lines {
                    self.processLogLine(line)
                }
                
                DispatchQueue.main.async { [weak self] in
                    self?.lastParseTime = Date()
                }
            } catch {
                ErrorReporter.shared.report(error, context: "BackgroundTaskScheduler.processNewLogContent", severity: .warning)
            }
        }
    }
    
    private func processLogLine(_ line: String) {
        guard let parsed = logParser.parseLine(line) else { return }
        
        // Check cache
        let cacheKey = "\(parsed.timestamp.timeIntervalSince1970)_\(parsed.type)"
        if parsedCache.get(cacheKey) != nil {
            return // Already processed
        }
        
        // Handle OSC notifications
        if parsed.type == .terminalNotification {
            let payload = parsed.command ?? ""
            let parts = payload.split(separator: "|", maxSplits: 1).map(String.init)
            let title = parts.count > 0 ? parts[0] : "Terminal Notification"
            let message = parts.count > 1 ? parts[1] : ""
            
            let notification = AppNotification(
                type: .terminalNotification,
                title: title,
                message: message,
                timestamp: parsed.timestamp
            )
            DispatchQueue.main.async { [weak self] in
                self?.eventBus.publish(notification)
            }
            // Still create an event for it? Optional but let's just return as it's handled.
            parsedCache.set(cacheKey, value: GhosttyEvent(timestamp: parsed.timestamp, type: .system, metadata: [:])) // Dummy event for cache
            return
        }
        
        // Create event
        let event = GhosttyEvent(
            timestamp: parsed.timestamp,
            type: mapLogLineType(parsed.type),
            command: parsed.command,
            output: nil,
            metadata: [:]
        )
        
        // Cache it
        parsedCache.set(cacheKey, value: event)
        
        // Publish event
        eventBus.publish(event)
        
        // Check for notifications
        checkAndCreateNotification(from: line, timestamp: parsed.timestamp)
        
        // Create log entry
        if let command = parsed.command {
            let logEntry = LogEntry(
                timestamp: parsed.timestamp,
                command: command,
                output: "",
                exitCode: parsed.exitCode
            )
            
            Task {
                try? await storageManager.appendLog(logEntry)
            }
        }
        
        self.eventsLock.lock()
        let newCount = self.eventsProcessed + 1
        self.eventsLock.unlock()
        DispatchQueue.main.async { [weak self] in
            self?.eventsProcessed = newCount
        }
    }
    
    private func checkAndCreateNotification(from content: String, timestamp: Date) {
        var notification: AppNotification?
        
        if aiDetector.matches(content) {
            if shouldThrottle(.aiOutput) { return }
            notification = AppNotification(
                type: .aiOutput,
                title: "AI Response",
                message: String(content.prefix(100)),
                timestamp: timestamp
            )
        } else if taskDetector.matches(content) {
            if shouldThrottle(.taskCompletion) { return }
            notification = AppNotification(
                type: .taskCompletion,
                title: "Task Completed",
                message: String(content.prefix(100)),
                timestamp: timestamp
            )
        } else if devWorkflowParser.matches(content) {
            if shouldThrottle(.devWorkflow) { return }
            notification = AppNotification(
                type: .devWorkflow,
                title: "Workflow Event",
                message: String(content.prefix(100)),
                timestamp: timestamp
            )
        }
        
        if let notification = notification {
            DispatchQueue.main.async { [weak self] in
                self?.eventBus.publish(notification)
            }
        }
    }
    
    private func shouldThrottle(_ type: NotificationType) -> Bool {
        let now = Date()
        if let lastTime = notificationThrottle[type],
            now.timeIntervalSince(lastTime) < settings.notifications.throttleInterval {
            return true
        }
        notificationThrottle[type] = now
        return false
    }
    
    private func mapLogLineType(_ type: LogLineType) -> EventType {
        switch type {
        case .commandStart, .commandEnd:
            return .command
        case .output, .aiResponse, .taskComplete, .build, .test, .terminalNotification:
            return .output
        }
    }
}

// MARK: - LRU Cache

/// Simple LRU cache for parsed log entries
final class LRUCache<Key: Hashable, Value> {
    private var cache: [Key: Value] = [:]
    private var order: [Key] = []
    private let capacity: Int
    private let lock = NSLock()
    
    init(capacity: Int) {
        self.capacity = capacity
    }
    
    func get(_ key: Key) -> Value? {
        lock.lock()
        defer { lock.unlock() }
        
        guard let value = cache[key] else { return nil }
        
        // Move to end (most recently used)
        if let index = order.firstIndex(of: key) {
            order.remove(at: index)
            order.append(key)
        }
        
        return value
    }
    
    func set(_ key: Key, value: Value) {
        lock.lock()
        defer { lock.unlock() }
        
        if cache[key] != nil {
            // Update existing
            cache[key] = value
            if let index = order.firstIndex(of: key) {
                order.remove(at: index)
                order.append(key)
            }
        } else {
            // Evict if at capacity
            if order.count >= capacity {
                if let oldest = order.first {
                    order.removeFirst()
                    cache.removeValue(forKey: oldest)
                }
            }
            
            cache[key] = value
            order.append(key)
        }
    }
    
    func clear() {
        lock.lock()
        defer { lock.unlock() }
        cache.removeAll()
        order.removeAll()
    }
}

// MARK: - Circular Buffer

/// Fixed-size circular buffer for performance samples
private struct CircularBuffer {
    private var buffer: [TimeInterval]
    private var writeIndex = 0
    private var count = 0
    private let capacity: Int
    
    init(capacity: Int) {
        self.capacity = capacity
        self.buffer = [TimeInterval](repeating: 0, count: capacity)
    }
    
    mutating func append(_ value: TimeInterval) {
        buffer[writeIndex] = value
        writeIndex = (writeIndex + 1) % capacity
        if count < capacity {
            count += 1
        }
    }
    
    var values: [TimeInterval] {
        if count < capacity {
            return Array(buffer.prefix(count))
        }
        // Return in order: oldest to newest
        return Array(buffer[writeIndex...]) + Array(buffer[..<writeIndex])
    }
    
    var isEmpty: Bool { count == 0 }
    var currentCount: Int { count }
}

// MARK: - Performance Monitor

/// Simple performance monitoring
final class PerformanceMonitor {
    static let shared = PerformanceMonitor()
    
    private var metrics: [String: CircularBuffer] = [:]
    private let lock = NSLock()
    private let bufferCapacity = 100
    
    private init() {}
    
    func measure(_ name: String, block: () -> Void) {
        let start = CFAbsoluteTimeGetCurrent()
        block()
        let duration = CFAbsoluteTimeGetCurrent() - start
        record(name, duration: duration)
    }
    
    func record(_ name: String, duration: TimeInterval) {
        lock.lock()
        defer { lock.unlock() }
        
        if metrics[name] == nil {
            metrics[name] = CircularBuffer(capacity: bufferCapacity)
        }
        metrics[name]?.append(duration)
    }
    
    func averageDuration(_ name: String) -> TimeInterval? {
        lock.lock()
        defer { lock.unlock() }
        
        guard let buffer = metrics[name], !buffer.isEmpty else { return nil }
        let values = buffer.values
        return values.reduce(0, +) / Double(values.count)
    }
    
    func report() -> String {
        lock.lock()
        defer { lock.unlock() }
        
        var report = "Performance Report:\n"
        for (name, buffer) in metrics.sorted(by: { $0.key < $1.key }) {
            let values = buffer.values
            let avg = values.reduce(0, +) / Double(values.count)
            report += "  \(name): avg=\(String(format: "%.3f", avg * 1000))ms, count=\(buffer.currentCount)\n"
        }
        return report
    }
}
