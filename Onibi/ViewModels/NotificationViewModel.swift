import Foundation
import Combine

/// ViewModel for managing notifications in the menubar
final class NotificationViewModel: ObservableObject {
    @Published var notifications: [AppNotification] = []
    @Published var showLogsView: Bool = false
    @Published var isLoading: Bool = false
    
    private let eventBus = EventBus.shared
    private var cancellables = Set<AnyCancellable>()
    
    init() {
        setupSubscriptions()
        loadPersistedNotifications()
    }
    
    /// Dismiss a single notification
    func dismiss(_ notification: AppNotification) {
        withAnimation(.easeOut(duration: 0.2)) {
            notifications.removeAll { $0.id == notification.id }
        }
        saveNotifications()
    }
    
    /// Clear all notifications
    func clearAll() {
        withAnimation(.easeOut(duration: 0.3)) {
            notifications.removeAll()
        }
        saveNotifications()
    }
    
    /// Mark notification as read
    func markAsRead(_ notification: AppNotification) {
        if let index = notifications.firstIndex(where: { $0.id == notification.id }) {
            notifications[index].isRead = true
            saveNotifications()
        }
    }
    
    /// Group notifications by type
    func groupedNotifications() -> [NotificationType: [AppNotification]] {
        Dictionary(grouping: notifications) { $0.type }
    }
    
    /// Get unread count
    var unreadCount: Int {
        notifications.filter { !$0.isRead }.count
    }
    
    // MARK: - Private
    
    private func setupSubscriptions() {
        eventBus.notificationPublisher
            .receive(on: DispatchQueue.main)
            .sink { [weak self] notification in
                self?.addNotification(notification)
            }
            .store(in: &cancellables)
    }
    
    private func addNotification(_ notification: AppNotification) {
        withAnimation(.spring(response: 0.3, dampingFraction: 0.8)) {
            notifications.insert(notification, at: 0)
        }
        
        // Enforce maximum count
        if notifications.count > 100 {
            notifications = Array(notifications.prefix(100))
        }
        
        saveNotifications()
    }
    
    private func loadPersistedNotifications() {
        guard let data = UserDefaults.standard.data(forKey: UserDefaultsKeys.notifications) else { return }
        
        do {
            notifications = try JSONDecoder().decode([AppNotification].self, from: data)
        } catch {
            ErrorReporter.shared.report(error, context: "NotificationViewModel loadNotifications")
        }
    }
    
    private func saveNotifications() {
        guard let data = try? JSONEncoder().encode(notifications) else { return }
        UserDefaults.standard.set(data, forKey: UserDefaultsKeys.notifications)
    }
}

// MARK: - AppNotification Codable

extension AppNotification: Codable {
    enum CodingKeys: String, CodingKey {
        case id, type, title, message, timestamp, isRead, sourceEvent
    }
    
    public init(from decoder: Decoder) throws {
        let container = try decoder.container(keyedBy: CodingKeys.self)
        id = try container.decode(UUID.self, forKey: .id)
        type = try container.decode(NotificationType.self, forKey: .type)
        title = try container.decode(String.self, forKey: .title)
        message = try container.decode(String.self, forKey: .message)
        timestamp = try container.decode(Date.self, forKey: .timestamp)
        isRead = try container.decode(Bool.self, forKey: .isRead)
        sourceEvent = try container.decodeIfPresent(GhosttyEvent.self, forKey: .sourceEvent)
    }
    
    public func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)
        try container.encode(id, forKey: .id)
        try container.encode(type, forKey: .type)
        try container.encode(title, forKey: .title)
        try container.encode(message, forKey: .message)
        try container.encode(timestamp, forKey: .timestamp)
        try container.encode(isRead, forKey: .isRead)
        try container.encodeIfPresent(sourceEvent, forKey: .sourceEvent)
    }
}
