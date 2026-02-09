import Foundation
import CryptoKit
import Combine

/// Utility for reducing false positive notifications
final class FalsePositiveReducer: ObservableObject {
    static let shared = FalsePositiveReducer()
    
    @Published var suppressedPatterns: [String] = []
    @Published var dismissalCounts: [NotificationType: Int] = [:]
    
    private var recentHashes: [String: Date] = [:]
    private let deduplicationWindow: TimeInterval = 5.0
    private let minimumContentLength = 3
    private var cleanupTimer: Timer?
    
    private var settings: Settings = .default
    
    private init() {
        loadSuppressedPatterns()
        
        // Subscribe to settings
        EventBus.shared.settingsPublisher
            .sink { [weak self] newSettings in
                self?.settings = newSettings
            }
            .store(in: &cancellables)
        
        // Start periodic cleanup timer (every 10 seconds)
        cleanupTimer = Timer.scheduledTimer(withTimeInterval: 10.0, repeats: true) { [weak self] _ in
            self?.cleanupOldHashes()
        }
    }
    
    deinit {
        cleanupTimer?.invalidate()
    }
    
    private var cancellables = Set<AnyCancellable>()
    
    /// Periodic cleanup of old hashes
    private func cleanupOldHashes() {
        let now = Date()
        recentHashes = recentHashes.filter { now.timeIntervalSince($0.value) < deduplicationWindow }
    }
    
    // MARK: - Detection Result with Confidence
    
    struct DetectionResult {
        let matched: Bool
        let confidence: Double // 0.0 to 1.0
        let type: NotificationType?
        let context: DetectionContext?
    }
    
    struct DetectionContext {
        let linesBefore: [String]
        let linesAfter: [String]
        let matchedPattern: String?
    }
    
    // MARK: - Confidence Scoring
    
    /// Calculate confidence score based on pattern specificity
    func calculateConfidence(
        for content: String,
        matchedPattern: String,
        isRegex: Bool,
        context: DetectionContext?
    ) -> Double {
        var score = 0.5 // Base score
        
        // Pattern specificity
        if isRegex {
            score += 0.2 // Regex patterns are more specific
        }
        
        // Pattern length (longer = more specific)
        if matchedPattern.count > 10 {
            score += 0.1
        }
        
        // Context confirmation
        if let ctx = context {
            if hasConfirmingContext(ctx) {
                score += 0.15
            }
        }
        
        // Content length (very short = less reliable)
        if content.count < 10 {
            score -= 0.2
        }
        
        return min(max(score, 0.0), 1.0)
    }
    
    /// Check context lines for confirming signals
    private func hasConfirmingContext(_ context: DetectionContext) -> Bool {
        let allContext = context.linesBefore + context.linesAfter
        
        let confirmingPatterns = [
            "completed", "finished", "done", "success", "passed",
            "error", "failed", "warning", "build", "test"
        ]
        
        for line in allContext {
            for pattern in confirmingPatterns {
                if line.lowercased().contains(pattern) {
                    return true
                }
            }
        }
        
        return false
    }
    
    // MARK: - Deduplication
    
    /// Check if content was recently seen (within window)
    func isDuplicate(_ content: String) -> Bool {
        let hash = contentHash(content)
        
        if recentHashes[hash] != nil {
            return true
        }
        
        recentHashes[hash] = Date()
        return false
    }
    
    private func contentHash(_ content: String) -> String {
        let data = Data(content.utf8)
        let hash = SHA256.hash(data: data)
        return hash.compactMap { String(format: "%02x", $0) }.joined().prefix(16).description
    }
    
    // MARK: - Content Length Filter
    
    /// Check if content meets minimum length requirement
    func meetsMinimumLength(_ content: String) -> Bool {
        let trimmed = content.trimmingCharacters(in: .whitespacesAndNewlines)
        return trimmed.count >= minimumContentLength
    }
    
    // MARK: - False Positive Management
    
    /// Mark a pattern as false positive
    func markAsFalsePositive(_ pattern: String) {
        if !suppressedPatterns.contains(pattern) {
            suppressedPatterns.append(pattern)
            saveSuppressedPatterns()
        }
    }
    
    /// Check if content matches any suppressed patterns
    func isSuppressed(_ content: String) -> Bool {
        for pattern in suppressedPatterns {
            if content.lowercased().contains(pattern.lowercased()) {
                return true
            }
        }
        return false
    }
    
    /// Remove a suppressed pattern
    func removeSuppression(_ pattern: String) {
        suppressedPatterns.removeAll { $0 == pattern }
        saveSuppressedPatterns()
    }
    
    // MARK: - Adaptive Throttling
    
    /// Record a dismissal for adaptive throttling
    func recordDismissal(type: NotificationType) {
        dismissalCounts[type, default: 0] += 1
    }
    
    /// Get suggested throttle interval based on dismissal frequency
    func suggestedThrottleInterval(for type: NotificationType) -> TimeInterval {
        let baseInterval: TimeInterval = 1.0
        let dismissals = dismissalCounts[type] ?? 0
        
        // Increase throttle interval for frequently dismissed types
        if dismissals > 10 {
            return baseInterval * 4.0
        } else if dismissals > 5 {
            return baseInterval * 2.0
        }
        
        return baseInterval
    }
    
    /// Reset dismissal counts (e.g., on app launch or daily)
    func resetDismissalCounts() {
        dismissalCounts.removeAll()
    }
    
    // MARK: - Persistence
    
    private let suppressedKey = "suppressedFalsePositivePatterns"
    
    private func loadSuppressedPatterns() {
        if let patterns = UserDefaults.standard.stringArray(forKey: suppressedKey) {
            suppressedPatterns = patterns
        }
    }
    
    private func saveSuppressedPatterns() {
        UserDefaults.standard.set(suppressedPatterns, forKey: suppressedKey)
    }
    
    // MARK: - Combined Filter
    
    /// Apply all false positive reduction filters
    func shouldNotify(
        content: String,
        pattern: String? = nil,
        type: NotificationType? = nil,
        context: DetectionContext? = nil
    ) -> DetectionResult {
        // Check minimum length
        guard meetsMinimumLength(content) else {
            return DetectionResult(matched: false, confidence: 0, type: type, context: context)
        }
        
        // Check suppression
        guard !isSuppressed(content) else {
            return DetectionResult(matched: false, confidence: 0, type: type, context: context)
        }
        
        // Check deduplication
        guard !isDuplicate(content) else {
            return DetectionResult(matched: false, confidence: 0, type: type, context: context)
        }
        
        // Calculate confidence
        let confidence = calculateConfidence(
            for: content,
            matchedPattern: pattern ?? "",
            isRegex: false,
            context: context
        )
        
        if confidence < settings.detectionThreshold {
            return DetectionResult(matched: false, confidence: confidence, type: type, context: context)
        }
        
        return DetectionResult(matched: true, confidence: confidence, type: type, context: context)
    }
}
