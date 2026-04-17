import Foundation

/// Sliding-window failure counter keyed by peer identifier (IP, or tunnel's
/// forwarded-for). In-memory only — state resets on process restart.
public actor AuthFailureTracker {
    public struct Decision: Equatable, Sendable {
        public let shouldBlock: Bool
        public let retryAfterSeconds: Int
    }

    private struct Bucket {
        /// Failure timestamps within the current window.
        var failures: [Date] = []
    }

    private var buckets: [String: Bucket] = [:]
    private let windowSeconds: TimeInterval
    private let maxFailures: Int

    public init(windowSeconds: TimeInterval = 60, maxFailures: Int = 5) {
        self.windowSeconds = windowSeconds
        self.maxFailures = maxFailures
    }

    /// Returns whether the peer is allowed right now. Called BEFORE auth check.
    /// Returns retryAfter so the router can surface it to the client.
    public func evaluate(peer: String, now: Date = Date()) -> Decision {
        prune(peer: peer, now: now)
        guard let bucket = buckets[peer] else {
            return Decision(shouldBlock: false, retryAfterSeconds: 0)
        }
        if bucket.failures.count >= maxFailures,
           let oldest = bucket.failures.first {
            let retry = max(1, Int(windowSeconds - now.timeIntervalSince(oldest)))
            return Decision(shouldBlock: true, retryAfterSeconds: retry)
        }
        return Decision(shouldBlock: false, retryAfterSeconds: 0)
    }

    /// Record that this peer just failed auth. Called only on auth-failure.
    public func recordFailure(peer: String, now: Date = Date()) {
        prune(peer: peer, now: now)
        var bucket = buckets[peer] ?? Bucket()
        bucket.failures.append(now)
        buckets[peer] = bucket
    }

    /// A successful auth clears the failure streak for that peer.
    public func recordSuccess(peer: String) {
        buckets.removeValue(forKey: peer)
    }

    /// Test helper: returns the current failure count (post-prune).
    public func failureCount(peer: String, now: Date = Date()) -> Int {
        prune(peer: peer, now: now)
        return buckets[peer]?.failures.count ?? 0
    }

    private func prune(peer: String, now: Date) {
        guard var bucket = buckets[peer] else { return }
        let threshold = now.addingTimeInterval(-windowSeconds)
        bucket.failures.removeAll { $0 < threshold }
        if bucket.failures.isEmpty {
            buckets.removeValue(forKey: peer)
        } else {
            buckets[peer] = bucket
        }
    }
}
