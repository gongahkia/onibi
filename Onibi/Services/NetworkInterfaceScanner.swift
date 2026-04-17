import Foundation
import Darwin

/// Non-loopback IPv4 interface discovered on the host (used for LAN pairing hints).
struct LocalNetworkInterface: Equatable, Hashable {
    let name: String
    let ipv4: String
    let isVirtual: Bool
    let isPrimary: Bool
}

enum NetworkInterfaceScanner {
    /// Prefixes for Apple-internal / VPN / virtual interfaces we hide by default.
    static let virtualPrefixes: [String] = ["utun", "llw", "awdl", "gif", "stf", "bridge", "ap", "vmnet"]

    static func isVirtualInterface(name: String) -> Bool {
        virtualPrefixes.contains(where: { name.hasPrefix($0) })
    }

    /// Returns non-loopback IPv4 addresses on active interfaces.
    /// - Parameter includeVirtual: when false, hides utun/awdl/etc.
    /// Results sorted with primary first, then physical, then virtual; stable within each group.
    static func ipv4Interfaces(includeVirtual: Bool = false) -> [LocalNetworkInterface] {
        let primary = primaryInterfaceName()

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
            let virtual = isVirtualInterface(name: name)
            if !includeVirtual && virtual { continue }

            results.append(LocalNetworkInterface(
                name: name,
                ipv4: ipv4,
                isVirtual: virtual,
                isPrimary: primary.map { $0 == name } ?? false
            ))
        }

        return results.sorted(by: sortOrder)
    }

    static func sortOrder(_ lhs: LocalNetworkInterface, _ rhs: LocalNetworkInterface) -> Bool {
        if lhs.isPrimary != rhs.isPrimary { return lhs.isPrimary }
        if lhs.isVirtual != rhs.isVirtual { return !lhs.isVirtual }
        if lhs.name == rhs.name { return lhs.ipv4 < rhs.ipv4 }
        return lhs.name < rhs.name
    }

    /// Best-effort: parses `route -n get default` output to find the interface name
    /// carrying the default route. Returns nil if unavailable.
    static func primaryInterfaceName() -> String? {
        let task = Process()
        task.executableURL = URL(fileURLWithPath: "/sbin/route")
        task.arguments = ["-n", "get", "default"]
        let stdout = Pipe()
        let stderr = Pipe()
        task.standardOutput = stdout
        task.standardError = stderr

        do {
            try task.run()
            task.waitUntilExit()
        } catch {
            return nil
        }

        guard task.terminationStatus == 0 else { return nil }
        let data = stdout.fileHandleForReading.readDataToEndOfFile()
        guard let output = String(data: data, encoding: .utf8) else { return nil }

        for line in output.split(separator: "\n") {
            let trimmed = line.trimmingCharacters(in: .whitespaces)
            if trimmed.hasPrefix("interface:") {
                return String(trimmed.dropFirst("interface:".count)).trimmingCharacters(in: .whitespaces)
            }
        }
        return nil
    }
}
