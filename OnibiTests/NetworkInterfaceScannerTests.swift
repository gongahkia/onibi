import XCTest
@testable import Onibi

final class NetworkInterfaceScannerTests: XCTestCase {
    func testIsVirtualRecognisesKnownPrefixes() {
        XCTAssertTrue(NetworkInterfaceScanner.isVirtualInterface(name: "utun3"))
        XCTAssertTrue(NetworkInterfaceScanner.isVirtualInterface(name: "awdl0"))
        XCTAssertTrue(NetworkInterfaceScanner.isVirtualInterface(name: "llw0"))
        XCTAssertTrue(NetworkInterfaceScanner.isVirtualInterface(name: "bridge100"))
        XCTAssertFalse(NetworkInterfaceScanner.isVirtualInterface(name: "en0"))
        XCTAssertFalse(NetworkInterfaceScanner.isVirtualInterface(name: "en1"))
    }

    func testSortOrderPutsPrimaryFirstThenPhysicalThenVirtual() {
        let primary = LocalNetworkInterface(name: "en0", ipv4: "192.168.1.20", isVirtual: false, isPrimary: true)
        let physical = LocalNetworkInterface(name: "en1", ipv4: "10.0.0.5", isVirtual: false, isPrimary: false)
        let virtual = LocalNetworkInterface(name: "utun0", ipv4: "100.64.0.1", isVirtual: true, isPrimary: false)

        let sorted = [virtual, physical, primary].sorted(by: NetworkInterfaceScanner.sortOrder)
        XCTAssertEqual(sorted.map(\.name), ["en0", "en1", "utun0"])
    }

    func testIPv4InterfacesDefaultExcludesVirtual() {
        let physical = NetworkInterfaceScanner.ipv4Interfaces(includeVirtual: false)
        for iface in physical {
            XCTAssertFalse(iface.isVirtual, "virtual iface \(iface.name) leaked into non-virtual set")
        }
    }
}
