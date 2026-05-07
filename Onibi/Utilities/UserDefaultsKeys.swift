import Foundation

/// Centralized repository for UserDefaults keys
enum UserDefaultsKeys {
    /// Key for persisted app settings
    static let settings = "appSettings"
    
    /// Key for persisted notifications
    static let notifications = "notifications"
    
    /// Key for custom pattern rules
    static let customPatterns = "customPatterns"

    /// Key for enabling automatic GitHub release checks
    static let updatesAutoCheckEnabled = "onibi.updates.autoCheckEnabled"

    /// Key for the last GitHub release check timestamp
    static let updatesLastCheckAt = "onibi.updates.lastCheckAt"
}
