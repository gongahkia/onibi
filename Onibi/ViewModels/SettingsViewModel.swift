import Foundation
import Combine

/// ViewModel for managing app settings
final class SettingsViewModel: ObservableObject {
    static let shared = SettingsViewModel()
    
    @Published var settings: AppSettings {
        didSet {
            let validated = settings.validated()
            if validated != settings {
                settings = validated
                return
            }
            EventBus.shared.publish(settings)
        }
    }
    
    private let settingsKey = UserDefaultsKeys.settings
    private var cancellables = Set<AnyCancellable>()
    
    init() {
        self.settings = SettingsViewModel.loadSettings()
        
        // Debounce settings save to avoid excessive writes
        $settings
            .dropFirst()
            .debounce(for: .milliseconds(300), scheduler: DispatchQueue.main)
            .sink { [weak self] _ in
                self?.saveSettings()
            }
            .store(in: &cancellables)
    }
    
    /// Reset all settings to defaults
    func resetToDefaults() {
        settings = AppSettings.default
    }
    
    /// Import settings from JSON file
    func importSettings(from url: URL) throws {
        let data = try Data(contentsOf: url)
        let decoder = JSONDecoder()
        settings = try decoder.decode(AppSettings.self, from: data)
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
            let config = await OnibiConfigParser.fetchConfig()
            
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
    
    private static func loadSettings() -> AppSettings {
        guard let data = UserDefaults.standard.data(forKey: UserDefaultsKeys.settings),
              let settings = try? JSONDecoder().decode(AppSettings.self, from: data) else {
            return AppSettings.default
        }
        return settings
    }
}
