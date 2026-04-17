import Foundation
import Darwin

/// Non-loopback IPv4 interface discovered on the host (used for LAN pairing hints).
struct LocalNetworkInterface: Equatable, Hashable {
    let name: String
    let ipv4: String
}

enum NetworkInterfaceScanner {
    /// Returns non-loopback IPv4 addresses on active interfaces.
    /// Results are deduplicated by address; ordering is stable (sorted by interface name).
    static func ipv4Interfaces() -> [LocalNetworkInterface] {
        var head: UnsafeMutablePointer<ifaddrs>?
        guard getifaddrs(&head) == 0, let first = head else {
            return []
        }
        defer { freeifaddrs(head) }

        var seen: Set<String> = []
        var results: [LocalNetworkInterface] = []

        var cursor: UnsafeMutablePointer<ifaddrs>? = first
        while let entry = cursor {
            defer { cursor = entry.pointee.ifa_next }

            guard let addr = entry.pointee.ifa_addr,
                  addr.pointee.sa_family == sa_family_t(AF_INET) else {
                continue
            }

            let flags = Int32(entry.pointee.ifa_flags)
            let up = (flags & IFF_UP) != 0
            let running = (flags & IFF_RUNNING) != 0
            let loopback = (flags & IFF_LOOPBACK) != 0
            guard up, running, !loopback else { continue }

            var host = [CChar](repeating: 0, count: Int(NI_MAXHOST))
            let status = getnameinfo(
                addr,
                socklen_t(addr.pointee.sa_len),
                &host,
                socklen_t(host.count),
                nil,
                0,
                NI_NUMERICHOST
            )
            guard status == 0 else { continue }

            let ipv4 = String(cString: host)
            if ipv4.isEmpty || ipv4.hasPrefix("169.254.") { continue } // ignore link-local
            if seen.contains(ipv4) { continue }
            seen.insert(ipv4)

            let name = String(cString: entry.pointee.ifa_name)
            results.append(LocalNetworkInterface(name: name, ipv4: ipv4))
        }

        return results.sorted { lhs, rhs in
            if lhs.name == rhs.name { return lhs.ipv4 < rhs.ipv4 }
            return lhs.name < rhs.name
        }
    }
}
