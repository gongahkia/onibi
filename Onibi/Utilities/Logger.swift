import os.log
import Foundation
enum Log {
    private static let subsystem = Bundle.main.bundleIdentifier ?? "com.onibi"
    static let general = os.Logger(subsystem: subsystem, category: "general")
    static let scheduler = os.Logger(subsystem: subsystem, category: "scheduler")
    static let storage = os.Logger(subsystem: subsystem, category: "storage")
    static let notifications = os.Logger(subsystem: subsystem, category: "notifications")
    static let sessions = os.Logger(subsystem: subsystem, category: "sessions")
    static let errors = os.Logger(subsystem: subsystem, category: "errors")
    static let settings = os.Logger(subsystem: subsystem, category: "settings")
}
