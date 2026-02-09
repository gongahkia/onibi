import Foundation
import Combine

/// Central event bus for reactive updates using Combine
final class EventBus: ObservableObject {
    static let shared = EventBus()
    
    /// Publisher for new Ghostty events
    let eventPublisher = PassthroughSubject<GhosttyEvent, Never>()
    
    /// Publisher for new log entries
    let logPublisher = PassthroughSubject<LogEntry, Never>()
    
    /// Publisher for notification events
    let notificationPublisher = PassthroughSubject<AppNotification, Never>()
    
    /// Publisher for settings changes
    let settingsPublisher = PassthroughSubject<Settings, Never>()
    
    /// Publisher for errors
    let errorPublisher = PassthroughSubject<Error, Never>()
    
    private init() {}
    
    /// Publish a new event
    func publish(_ event: GhosttyEvent) {
        eventPublisher.send(event)
    }
    
    /// Publish a new log entry
    func publish(_ log: LogEntry) {
        logPublisher.send(log)
    }
    
    /// Publish a notification
    func publish(_ notification: AppNotification) {
        notificationPublisher.send(notification)
    }
    
    /// Publish settings update
    func publish(_ settings: Settings) {
        settingsPublisher.send(settings)
    }
    
    /// Publish an error
    func publish(_ error: Error) {
        errorPublisher.send(error)
    }
}

/// App notification model
struct AppNotification: Identifiable, Equatable {
    let id: UUID
    let type: NotificationType
    let title: String
    let message: String
    let timestamp: Date
    let sourceEvent: GhosttyEvent?
    var isRead: Bool
    
    init(
        id: UUID = UUID(),
        type: NotificationType,
        title: String,
        message: String,
        timestamp: Date = Date(),
        sourceEvent: GhosttyEvent? = nil,
        isRead: Bool = false
    ) {
        self.id = id
        self.type = type
        self.title = title
        self.message = message
        self.timestamp = timestamp
        self.sourceEvent = sourceEvent
        self.isRead = isRead
    }
    
    static func == (lhs: AppNotification, rhs: AppNotification) -> Bool {
        lhs.id == rhs.id
    }
}
