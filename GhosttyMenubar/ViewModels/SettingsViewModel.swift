import Foundation
import Combine

/// ViewModel for managing app settings
final class SettingsViewModel: ObservableObject {
    @Published var settings: Settings {
        didSet {
            saveSettings()
            EventBus.shared.publish(settings)
        }
    }
    
    private let settingsKey = "appSettings"
    
    init() {
        self.settings = SettingsViewModel.loadSettings()
    }
    
    /// Reset all settings to defaults
    func resetToDefaults() {
        settings = Settings.default
    }
    
    /// Import settings from JSON file
    func importSettings(from url: URL) throws {
        let data = try Data(contentsOf: url)
        let decoder = JSONDecoder()
        settings = try decoder.decode(Settings.self, from: data)
    }
    
    /// Export settings to JSON file
    func exportSettings(to url: URL) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        let data = try encoder.encode(settings)
        try data.write(to: url)
    }
    
    // MARK: - Private
    
    private func saveSettings() {
        do {
            let encoder = JSONEncoder()
            let data = try encoder.encode(settings)
            UserDefaults.standard.set(data, forKey: settingsKey)
        } catch {
            print("Failed to save settings: \(error)")
        }
    }
    
    private static func loadSettings() -> Settings {
        guard let data = UserDefaults.standard.data(forKey: "appSettings"),
              let settings = try? JSONDecoder().decode(Settings.self, from: data) else {
            return Settings.default
        }
        return settings
    }
}
