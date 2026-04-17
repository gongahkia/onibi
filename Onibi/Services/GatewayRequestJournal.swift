import Foundation
import Combine

struct GatewayRequestEntry: Identifiable, Equatable {
    let id = UUID()
    let timestamp: Date
    let method: String
    let path: String
    let statusCode: Int
    let latencyMs: Int
    let peer: String

    var isSuccess: Bool { (200...299).contains(statusCode) }
}

/// Rolling in-memory journal of HTTP/WebSocket-upgrade requests served by the
/// gateway. Never persisted — purely for in-app observability.
final class GatewayRequestJournal: ObservableObject {
    static let shared = GatewayRequestJournal()

    @Published private(set) var entries: [GatewayRequestEntry] = []
    private let lock = NSLock()
    private var backing: [GatewayRequestEntry] = []
    private let capacity: Int

    init(capacity: Int = 200) {
        self.capacity = capacity
    }

    func record(
        method: String,
        path: String,
        statusCode: Int,
        latencyMs: Int,
        peer: String
    ) {
        let entry = GatewayRequestEntry(
            timestamp: Date(),
            method: method,
            path: path,
            statusCode: statusCode,
            latencyMs: latencyMs,
            peer: peer
        )

        lock.lock()
        backing.insert(entry, at: 0)
        if backing.count > capacity {
            backing.removeLast(backing.count - capacity)
        }
        let snapshot = backing
        lock.unlock()

        DispatchQueue.main.async {
            self.entries = snapshot
        }
    }

    func clear() {
        lock.lock()
        backing = []
        lock.unlock()
        DispatchQueue.main.async {
            self.entries = []
        }
    }

    func snapshot() -> [GatewayRequestEntry] {
        lock.lock()
        defer { lock.unlock() }
        return backing
    }
}
