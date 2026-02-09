import Foundation
import Combine

/// ViewModel for managing app settings
final class SettingsViewModel: ObservableObject {
    static let shared = SettingsViewModel()
    
    @Published var settings: Settings
    
    private let settingsKey = UserDefaultsKeys.settings
    private var cancellables = Set<AnyCancellable>()
    
    init() {
        self.settings = SettingsViewModel.loadSettings()
        
        // Debounce settings save by 0.3s to reduce file I/O
        $settings
            .dropFirst() // Skip initial value
            .debounce(for: .seconds(0.3), scheduler: DispatchQueue.main)
            .sink { [weak self] newSettings in
                guard let self = self else { return }
                let validated = newSettings.validated()
                if validated != newSettings {
                    self.settings = validated
                    return
                }
                self.saveSettings()
                EventBus.shared.publish(newSettings)
            }
            .store(in: &cancellables)
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
    
    func exportSettings(to url: URL) throws {
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        let data = try encoder.encode(settings)
        try data.write(to: url)
    }
    
    /// Sync theme from Ghostty configuration
    func syncGhosttyTheme() {
        Task {
            let config = await GhosttyConfigParser.fetchConfig()
            
            // Extract colors
            if let bg = config.backgroundColor, let fg = config.foregroundColor {
                let customTheme = CustomTheme(
                    backgroundColor: bg,
                    foregroundColor: fg,
                    accentColor: config.cursorColor
                )
                
                await MainActor.run {
                    self.settings.customTheme = customTheme
                    self.settings.syncThemeWithGhostty = true
                }
            }
        }
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
        guard let data = UserDefaults.standard.data(forKey: UserDefaultsKeys.settings),
              let settings = try? JSONDecoder().decode(Settings.self, from: data) else {
            return Settings.default
        }
        return settings
    }
}
