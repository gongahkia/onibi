import Foundation
import Combine

@MainActor
final class ConnectedClientsViewModel: ObservableObject {
    static let shared = ConnectedClientsViewModel()

    @Published private(set) var clients: [RealtimeGatewayService.ClientInfo] = []
    @Published private(set) var lastRefreshedAt: Date?

    private var refreshTask: Task<Void, Never>?

    func refresh() {
        refreshTask?.cancel()
        refreshTask = Task {
            let snapshot = await RealtimeGatewayService.shared.clientsSnapshot()
            if Task.isCancelled { return }
            self.clients = snapshot
            self.lastRefreshedAt = Date()
        }
    }

    func kick(_ id: UUID) {
        Task {
            await RealtimeGatewayService.shared.kick(clientID: id)
            self.refresh()
        }
    }
}
