import Foundation
import Combine

/// ViewModel for the detailed logs viewer
final class LogsViewModel: ObservableObject {
    @Published var logs: [LogEntry] = [] {
        didSet {
            statisticsDirty = true
        }
    }
    @Published var isLoading: Bool = false
    @Published var activeFilters: [String] = []
    @Published var errorMessage: String?
    
    private let eventBus = EventBus.shared
    private let storageManager = JSONStorageManager.shared
    private var cancellables = Set<AnyCancellable>()
    
    private var statisticsDirty = true
    private var cachedStatistics = LogStatistics(totalCommands: 0, successfulCommands: 0, failedCommands: 0, aiQueries: 0, taskCompletions: 0)
    
    init() {
        setupSubscriptions()
        loadLogs()
    }
    
    /// Load logs from storage
    func loadLogs() {
        isLoading = true
        Task { [weak self] in
            guard let self else { return }
            let loadedLogs = (try? await storageManager.loadLogs()) ?? []
            await MainActor.run {
                self.logs = loadedLogs.sorted { $0.sortTimestamp > $1.sortTimestamp }
                self.isLoading = false
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
    
    /// Get statistics (cached with dirty flag)
    var statistics: LogStatistics {
        if statisticsDirty {
            cachedStatistics = LogStatistics(
                totalCommands: logs.count,
                successfulCommands: logs.filter { $0.exitCode == 0 }.count,
                failedCommands: logs.filter { ($0.exitCode ?? 0) != 0 }.count,
                aiQueries: logs.filter { $0.isAssistantCommand }.count,
                taskCompletions: logs.filter { $0.metadata["source"] == "task" }.count
            )
            statisticsDirty = false
        }
        return cachedStatistics
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
