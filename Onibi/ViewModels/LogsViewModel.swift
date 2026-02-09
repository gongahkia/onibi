import Foundation
import Combine

/// ViewModel for the detailed logs viewer
final class LogsViewModel: ObservableObject {
    @Published var logs: [LogEntry] = []
    @Published var isLoading: Bool = false
    @Published var activeFilters: [String] = []
    @Published var errorMessage: String?
    
    private let eventBus = EventBus.shared
    private var cancellables = Set<AnyCancellable>()
    
    init() {
        setupSubscriptions()
        loadLogs()
    }
    
    /// Load logs from storage
    func loadLogs() {
        isLoading = true
        
        DispatchQueue.global(qos: .userInitiated).async { [weak self] in
            // Load from JSON storage
            let logsPath = OnibiConfig.appDataDirectory + "/logs.json"
            
            var loadedLogs: [LogEntry] = []
            
            if FileManager.default.fileExists(atPath: logsPath) {
                do {
                    let data = try Data(contentsOf: URL(fileURLWithPath: logsPath))
                    loadedLogs = try JSONDecoder().decode([LogEntry].self, from: data)
                } catch {
                    ErrorReporter.shared.report(error, context: "LogsViewModel.loadLogs JSON decode")
                }
            }
            
            DispatchQueue.main.async {
                self?.logs = loadedLogs.sorted { $0.timestamp > $1.timestamp }
                self?.isLoading = false
            }
        }
    }
    
    /// Add a filter
    func addFilter(_ filter: String) {
        if !activeFilters.contains(filter) {
            activeFilters.append(filter)
        }
    }
    
    /// Remove a filter
    func removeFilter(_ filter: String) {
        activeFilters.removeAll { $0 == filter }
    }
    
    /// Clear all filters
    func clearFilters() {
        activeFilters.removeAll()
    }
    
    /// Get statistics
    var statistics: LogStatistics {
        LogStatistics(
            totalCommands: logs.count,
            successfulCommands: logs.filter { $0.exitCode == 0 }.count,
            failedCommands: logs.filter { ($0.exitCode ?? 0) != 0 }.count,
            aiQueries: logs.filter { $0.metadata["source"] == "ai" }.count,
            taskCompletions: logs.filter { $0.metadata["source"] == "task" }.count
        )
    }
    
    // MARK: - Private
    
    private func setupSubscriptions() {
        eventBus.logPublisher
            .receive(on: DispatchQueue.main)
            .sink { [weak self] log in
                self?.logs.insert(log, at: 0)
            }
            .store(in: &cancellables)
    }
}

/// Log statistics summary
struct LogStatistics {
    let totalCommands: Int
    let successfulCommands: Int
    let failedCommands: Int
    let aiQueries: Int
    let taskCompletions: Int
    
    var successRate: Double {
        guard totalCommands > 0 else { return 0 }
        return Double(successfulCommands) / Double(totalCommands) * 100
    }
}
