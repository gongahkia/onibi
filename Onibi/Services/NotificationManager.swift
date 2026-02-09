import Foundation
import AppKit
import UserNotifications
import Combine

/// Native macOS notification manager wrapping UNUserNotificationCenter
/// Gracefully handles running outside of a proper .app bundle (e.g., via `swift build`)
final class NotificationManager: NSObject, ObservableObject {
    static let shared = NotificationManager()
    
    /// Lazy, safe initialization of UNUserNotificationCenter
    /// Returns nil if we're not running in a proper app bundle
    private lazy var center: UNUserNotificationCenter? = {
        // Check if we have a valid bundle identifier before attempting to access notification center
        guard Bundle.main.bundleIdentifier != nil else {
            Log.notifications.warning("No bundle identifier - notifications disabled (running outside .app bundle)")
            return nil
        }
        
        // Return the notification center - the bundle identifier check above prevents the crash
        return UNUserNotificationCenter.current()
    }()
    
    /// Whether native notifications are available (requires proper .app bundle)
    var isNotificationCenterAvailable: Bool {
        return center != nil
    }
    
    @Published var isAuthorized: Bool = false
    @Published var authorizationStatus: UNAuthorizationStatus = .notDetermined
    
    private var settings: AppSettings = .default
    private var cancellables = Set<AnyCancellable>()
    
    // Notification category identifiers
    enum Category: String {
        case system = "GHOSTTY_SYSTEM"
        case taskCompletion = "GHOSTTY_TASK"
        case aiOutput = "GHOSTTY_AI"
        case devWorkflow = "GHOSTTY_DEV"
        case automation = "GHOSTTY_AUTOMATION"
        
        static func from(_ type: NotificationType) -> Category {
            switch type {
            case .system: return .system
            case .taskCompletion: return .taskCompletion
            case .aiOutput: return .aiOutput
            case .devWorkflow: return .devWorkflow
            case .automation: return .automation
            case .terminalNotification: return .system
            }
        }
    }
    
    // Action identifiers
    enum Action: String {
        case viewInApp = "VIEW_IN_APP"
        case dismiss = "DISMISS"
        case openTerminal = "OPEN_TERMINAL"
    }
    
    override private init() {
        super.init()
        setupSubscriptions()
        
        // Only set up delegate and check authorization if center is available
        if let notificationCenter = center {
            notificationCenter.delegate = self
            checkAuthorizationStatus()
        } else {
            Log.notifications.info("Running without native notifications - use Xcode or build as .app bundle for full functionality")
        }
    }
    
    // MARK: - Authorization
    
    /// Request notification authorization
    func requestAuthorization() async -> Bool {
        guard let notificationCenter = center else {
            Log.notifications.debug("Skipping authorization request - notification center not available")
            return false
        }
        
        do {
            let granted = try await notificationCenter.requestAuthorization(options: [.alert, .sound, .badge])
            await MainActor.run {
                self.isAuthorized = granted
                self.authorizationStatus = granted ? .authorized : .denied
            }
            
            if granted {
                await registerCategories()
            }
            
            return granted
        } catch {
            Log.notifications.error("authorization error: \(error.localizedDescription)")
            return false
        }
    }
    
    /// Check current authorization status
    func checkAuthorizationStatus() {
        guard let notificationCenter = center else { return }
        
        notificationCenter.getNotificationSettings { [weak self] settings in
            DispatchQueue.main.async {
                self?.authorizationStatus = settings.authorizationStatus
                self?.isAuthorized = settings.authorizationStatus == .authorized
            }
        }
    }
    
    // MARK: - Categories
    
    /// Register notification categories with actions
    private func registerCategories() async {
        let viewAction = UNNotificationAction(
            identifier: Action.viewInApp.rawValue,
            title: "View in App",
            options: .foreground
        )
        
        let dismissAction = UNNotificationAction(
            identifier: Action.dismiss.rawValue,
            title: "Dismiss",
            options: .destructive
        )
        
        let openTerminalAction = UNNotificationAction(
            identifier: Action.openTerminal.rawValue,
            title: "Open Terminal",
            options: .foreground
        )
        
        let categories: [UNNotificationCategory] = Category.allCases.map { category in
            UNNotificationCategory(
                identifier: category.rawValue,
                actions: [viewAction, openTerminalAction, dismissAction],
                intentIdentifiers: [],
                options: .customDismissAction
            )
        }
        
        center?.setNotificationCategories(Set(categories))
    }
    
    // MARK: - Send Notifications
    
    /// Send a native notification from AppNotification
    func send(_ notification: AppNotification) async {
        guard let notificationCenter = center, isAuthorized else { return }
        
        let content = UNMutableNotificationContent()
        content.title = notification.title
        content.body = notification.message
        content.categoryIdentifier = Category.from(notification.type).rawValue
        content.threadIdentifier = notification.type.rawValue // Group by type
        content.sound = .default
        content.userInfo = [
            "notificationId": notification.id.uuidString,
            "type": notification.type.rawValue,
            "timestamp": notification.timestamp.timeIntervalSince1970
        ]
        
        // Add custom sound based on type
        if let soundName = soundName(for: notification.type) {
            content.sound = UNNotificationSound(named: UNNotificationSoundName(soundName))
        }
        
        let request = UNNotificationRequest(
            identifier: notification.id.uuidString,
            content: content,
            trigger: nil // Deliver immediately
        )
        
        do {
            try await notificationCenter.add(request)
        } catch {
            Log.notifications.error("failed to send notification: \(error.localizedDescription)")
        }
    }
    
    /// Remove delivered notifications
    func removeDelivered(ids: [String]) {
        center?.removeDeliveredNotifications(withIdentifiers: ids)
    }
    
    /// Remove all delivered notifications
    func removeAllDelivered() {
        center?.removeAllDeliveredNotifications()
    }
    
    /// Update badge count
    func setBadgeCount(_ count: Int) async {
        guard let notificationCenter = center else { return }
        
        do {
            try await notificationCenter.setBadgeCount(count)
        } catch {
            Log.notifications.error("failed to set badge: \(error.localizedDescription)")
        }
    }
    
    // MARK: - Private
    
    private func setupSubscriptions() {
        EventBus.shared.settingsPublisher
            .sink { [weak self] settings in
                self?.settings = settings
            }
            .store(in: &cancellables)
    }
    
    private func soundName(for type: NotificationType) -> String? {
        // Global override if set
        if let globalSound = settings.notifications.soundName {
            return globalSound
        }
        
        // Per-type sound
        return settings.notifications.soundMap[type]
    }
}

// MARK: - UNUserNotificationCenterDelegate

extension NotificationManager: UNUserNotificationCenterDelegate {
    /// Handle notification when app is in foreground
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification
    ) async -> UNNotificationPresentationOptions {
        // Show banner even when app is in foreground
        return [.banner, .sound, .badge]
    }
    
    /// Handle notification actions
    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse
    ) async {
        let userInfo = response.notification.request.content.userInfo
        let notificationId = userInfo["notificationId"] as? String
        
        switch response.actionIdentifier {
        case Action.viewInApp.rawValue, UNNotificationDefaultActionIdentifier:
            // Bring app to foreground and show notification
            await MainActor.run {
                NotificationCenter.default.post(
                    name: .showNotificationInApp,
                    object: nil,
                    userInfo: ["notificationId": notificationId ?? ""]
                )
            }
            
        case Action.openTerminal.rawValue:
            // Open Ghostty terminal
            await MainActor.run {
                NSWorkspace.shared.launchApplication("Ghostty")
            }
            
        case Action.dismiss.rawValue:
            // Already dismissed by the system
            break
            
        default:
            break
        }
    }
}

// MARK: - Category CaseIterable

extension NotificationManager.Category: CaseIterable {}

// MARK: - Notification Names

extension Notification.Name {
    static let showNotificationInApp = Notification.Name("showNotificationInApp")
}
