import Foundation

/// Parser for Ghostty configuration files
struct OnibiConfigParser {
    
    /// Parsed Ghostty configuration
    struct Config {
        var theme: String?
        var fontFamily: String?
        var fontSize: Int?
        var backgroundColor: String?
        var foregroundColor: String?
        var cursorColor: String?
        var selectionBackground: String?
        var windowDecorations: Bool?
        var shellIntegration: Bool?
        var customProperties: [String: String] = [:]
    }
    
    /// Parse Ghostty config from default locations
    static func parse() -> Config? {
        for path in OnibiConfig.configLocations {
            if FileManager.default.fileExists(atPath: path) {
                return parse(at: path)
            }
        }
        return nil
    }
    
    /// Parse config from specific path
    static func parse(at path: String) -> Config? {
        guard let contents = try? String(contentsOfFile: path, encoding: .utf8) else {
            return nil
        }
        return parse(contents: contents)
    }
    
    /// Parse config from dictionary
    static func parse(dictionary: [String: String]) -> Config {
        var config = Config()
        
        for (key, value) in dictionary {
            let normalizedKey = key.lowercased().replacingOccurrences(of: "-", with: "_")
            
            // Map known keys
            switch normalizedKey {
            case "theme":
                config.theme = value
            case "font_family":
                config.fontFamily = value
            case "font_size":
                if let size = Int(value), size > 0 {
                    config.fontSize = size
                }
            case "background":
                config.backgroundColor = value
            case "foreground":
                config.foregroundColor = value
            case "cursor_color":
                config.cursorColor = value
            case "selection_background":
                config.selectionBackground = value
            case "window_decorations":
                config.windowDecorations = value.lowercased() == "true" || value == "1"
            case "shell_integration":
                config.shellIntegration = value.lowercased() != "none" && value.lowercased() != "false"
            default:
                config.customProperties[normalizedKey] = value
            }
        }
        
        return config
    }
    
    /// Parse config from string contents
    static func parse(contents: String) -> Config {
        var dictionary: [String: String] = [:]
        
        let lines = contents.components(separatedBy: .newlines)
        
        for line in lines {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            
            // Skip comments and empty lines
            if trimmed.isEmpty || trimmed.hasPrefix("#") {
                continue
            }
            
            // Parse key = value or key value
            let parts: [String]
            if trimmed.contains("=") {
                parts = trimmed.components(separatedBy: "=").map { $0.trimmingCharacters(in: .whitespaces) }
            } else {
                parts = trimmed.components(separatedBy: .whitespaces).filter { !$0.isEmpty }
            }
            
            guard parts.count >= 2 else { continue }
            
            let key = parts[0]
            let value = parts.dropFirst().joined(separator: " ")
            dictionary[key] = value
        }
        
        return parse(dictionary: dictionary)
    }
    
    /// Fetch config asynchronously (CLI first, then file fallback)
    static func fetchConfig() async -> Config {
        // Try CLI first
        if let cliConfig = try? await GhosttyCliService.shared.showConfig() {
            return parse(dictionary: cliConfig)
        }
        
        // Fallback to file parsing
        if let fileConfig = parse() {
            return fileConfig
        }
        
        return Config()
    }
    
    /// Get color from Ghostty config format
    static func parseColor(_ value: String) -> (r: UInt8, g: UInt8, b: UInt8)? {
        var hex = value.trimmingCharacters(in: .whitespaces)
        
        // Remove # prefix if present
        if hex.hasPrefix("#") {
            hex = String(hex.dropFirst())
        }
        
        // Handle 6-digit hex
        guard hex.count == 6 else { return nil }
        
        var rgb: UInt64 = 0
        Scanner(string: hex).scanHexInt64(&rgb)
        
        return (
            r: UInt8((rgb >> 16) & 0xFF),
            g: UInt8((rgb >> 8) & 0xFF),
            b: UInt8(rgb & 0xFF)
        )
    }
}

// MARK: - Theme Colors

extension OnibiConfigParser.Config {
    /// Get SwiftUI-compatible color from background
    var backgroundColorRGB: (r: UInt8, g: UInt8, b: UInt8)? {
        guard let bg = backgroundColor else { return nil }
        return OnibiConfigParser.parseColor(bg)
    }
    
    /// Get SwiftUI-compatible color from foreground
    var foregroundColorRGB: (r: UInt8, g: UInt8, b: UInt8)? {
        guard let fg = foregroundColor else { return nil }
        return OnibiConfigParser.parseColor(fg)
    }
}
