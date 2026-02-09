import Foundation
import Combine
import SwiftUI

/// Manages terminal session tracking and lifecycle
final class SessionManager: ObservableObject {
    static let shared = SessionManager()
    
    @Published var activeSessions: [String: TerminalSession] = [:]
    @Published var recentSessions: [TerminalSession] = []
    
    private var cancellables = Set<AnyCancellable>()
    private let eventBus = EventBus.shared
    
    private init() {
        setupEventSubscription()
    }
    
    deinit {
        cancellables.removeAll()
    }
    
    // MARK: - Session Model
    
    struct TerminalSession: Identifiable, Equatable {
        let id: String
        var startTime: Date
        var lastActivityTime: Date
        var commandCount: Int
        var isActive: Bool
        var displayName: String?
        
        var formattedDuration: String {
            let duration = lastActivityTime.timeIntervalSince(startTime)
            let formatter = DateComponentsFormatter()
            formatter.unitsStyle = .abbreviated
            formatter.allowedUnits = [.hour, .minute, .second]
            return formatter.string(from: duration) ?? "0s"
        }
        
        /// Generate a display color based on session ID hash
        var displayColor: Color {
            let hash = abs(id.hashValue)
            let colors: [Color] = [.blue, .green, .orange, .purple, .pink, .teal, .indigo, .mint]
            return colors[hash % colors.count]
        }
    }
    
    // MARK: - Session Management
    
    /// Get or create a session
    func getOrCreateSession(id: String) -> TerminalSession {
        if let existing = activeSessions[id] {
            return existing
        }
        
        let session = TerminalSession(
            id: id,
            startTime: Date(),
            lastActivityTime: Date(),
            commandCount: 0,
            isActive: true
        )
        
        activeSessions[id] = session
        return session
    }
    
    /// Record activity for a session
    func recordActivity(sessionId: String) {
        guard var session = activeSessions[sessionId] else {
            // Create new session if doesn't exist
            var newSession = getOrCreateSession(id: sessionId)
            newSession.commandCount += 1
            newSession.lastActivityTime = Date()
            activeSessions[sessionId] = newSession
            return
        }
        
        session.lastActivityTime = Date()
        session.commandCount += 1
        session.isActive = true
        activeSessions[sessionId] = session
    }
    
    /// Mark session as inactive after timeout
    func markInactive(sessionId: String) {
        guard var session = activeSessions[sessionId] else { return }
        session.isActive = false
        activeSessions[sessionId] = session
        
        // Add to recent sessions
        if !recentSessions.contains(where: { $0.id == sessionId }) {
            recentSessions.insert(session, at: 0)
            // Keep only last 10 recent sessions
            if recentSessions.count > 10 {
                recentSessions.removeLast()
            }
        }
    }
    
    /// Check for inactive sessions (no activity for 5 minutes)
    func checkForInactiveSessions() {
        let timeout: TimeInterval = 300 // 5 minutes
        let now = Date()
        
        for (id, session) in activeSessions {
            if session.isActive && now.timeIntervalSince(session.lastActivityTime) > timeout {
                markInactive(sessionId: id)
            }
        }
    }
    
    /// Prune sessions inactive for > 24h
    func pruneStaleSessions() {
        let oneDay: TimeInterval = 86400 // 24 hours
        let now = Date()
        
        // Remove sessions that haven't been active for 24 hours
        let initialCount = activeSessions.count
        activeSessions = activeSessions.filter { _, session in
            now.timeIntervalSince(session.lastActivityTime) < oneDay
        }
        
        if activeSessions.count < initialCount {
            print("[SessionManager] Pruned \(initialCount - activeSessions.count) stale sessions")
        }
    }
    
    /// Get all session IDs
    var allSessionIds: [String] {
        Array(activeSessions.keys).sorted()
    }
    
    /// Get active session IDs only
    var activeSessionIds: [String] {
        activeSessions.filter { $0.value.isActive }.keys.sorted()
    }
    
    /// Clear all sessions
    func clearAll() {
        activeSessions.removeAll()
        recentSessions.removeAll()
    }
    
    // MARK: - Event Subscription
    
    private func setupEventSubscription() {
        // Subscribe to events and track sessions
        eventBus.eventPublisher
            .receive(on: DispatchQueue.main)
            .sink { [weak self] event in
                if let sessionId = event.sessionId {
                    self?.recordActivity(sessionId: sessionId)
                }
            }
            .store(in: &cancellables)
        
        // Periodic check for inactive sessions
        Timer.publish(every: 60, on: .main, in: .common)
            .autoconnect()
            .sink { [weak self] _ in
                self?.checkForInactiveSessions()
                self?.pruneStaleSessions()
            }
            .store(in: &cancellables)
    }
    
    // MARK: - Display Helpers
    
    /// Get display name for session
    func displayName(for sessionId: String) -> String {
        if let session = activeSessions[sessionId], let name = session.displayName {
            return name
        }
        // Shorten session ID for display
        if sessionId.count > 8 {
            return String(sessionId.prefix(8)) + "..."
        }
        return sessionId
    }
    
    /// Format session for notification context
    func sessionContext(for sessionId: String?) -> String? {
        guard let id = sessionId else { return nil }
        return "Session: \(displayName(for: id))"
    }
}
