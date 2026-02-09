import Foundation

/// User interface complexity preference
enum UserPersona: String, Codable, CaseIterable {
    case casual = "casual"
    case powerUser = "powerUser"
    
    var displayName: String {
        switch self {
        case .casual: return "Casual"
        case .powerUser: return "Power User"
        }
    }
    
    var description: String {
        switch self {
        case .casual:
            return "Simplified interface with fewer distractions. Best for most users."
        case .powerUser:
            return "Advanced controls, detailed logs, and full configuration access."
        }
    }
}
