import Foundation

/// Custom filtering rule for log entries
struct FilterRule: Identifiable, Codable, Equatable {
    let id: UUID
    var name: String
    var isEnabled: Bool
    var pattern: String
    var isRegex: Bool
    var matchType: MatchType
    var action: FilterAction
    
    init(
        id: UUID = UUID(),
        name: String,
        isEnabled: Bool = true,
        pattern: String,
        isRegex: Bool = false,
        matchType: MatchType = .contains,
        action: FilterAction = .highlight
    ) {
        self.id = id
        self.name = name
        self.isEnabled = isEnabled
        self.pattern = pattern
        self.isRegex = isRegex
        self.matchType = matchType
        self.action = action
    }
}

/// How to match the filter pattern
enum MatchType: String, Codable, CaseIterable {
    case contains
    case startsWith
    case endsWith
    case exact
    case regex
}

/// What action to take when filter matches
enum FilterAction: String, Codable, CaseIterable {
    case highlight
    case hide
    case notify
    case tag
}
