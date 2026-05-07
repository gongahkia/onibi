import Foundation
import XCTest
@testable import Onibi

@MainActor
final class UpdaterControllerTests: XCTestCase {
    func testNewerGitHubReleaseIsDetectedFromVTag() async throws {
        let defaults = makeDefaults()
        let networking = MockUpdaterNetworking(
            latestData: releaseJSON(tag: "v2.1.0", assetName: "Onibi.dmg")
        )
        let controller = UpdaterController(
            currentVersion: "2.0.0",
            networking: networking,
            defaults: defaults,
            downloadsDirectory: temporaryDownloadsDirectory()
        )

        controller.checkForUpdatesNow(trigger: .manual)
        try await waitFor { controller.availableRelease != nil }

        XCTAssertEqual(controller.availableRelease?.version, "2.1.0")
        XCTAssertEqual(controller.availableRelease?.asset.name, "Onibi.dmg")
    }

    func testEqualOrOlderReleaseDoesNotSetAvailableUpdate() async throws {
        for tag in ["v2.0.0", "v1.9.9"] {
            let defaults = makeDefaults()
            let networking = MockUpdaterNetworking(
                latestData: releaseJSON(tag: tag, assetName: "Onibi.dmg")
            )
            let controller = UpdaterController(
                currentVersion: "2.0.0",
                networking: networking,
                defaults: defaults,
                downloadsDirectory: temporaryDownloadsDirectory()
            )

            controller.checkForUpdatesNow(trigger: .manual)
            try await waitFor { !controller.isChecking }

            XCTAssertNil(controller.availableRelease)
            XCTAssertEqual(controller.downloadState, .idle)
        }
    }

    func testPreferredDMGAssetSelectionFallsBackToAnyDMG() throws {
        let preferred = try UpdaterController.decodeRelease(
            data: releaseJSON(
                tag: "v2.1.0",
                assets: [
                    ("Other.dmg", "https://example.com/Other.dmg"),
                    ("Onibi.dmg", "https://example.com/Onibi.dmg")
                ]
            )
        )
        XCTAssertEqual(preferred.asset.name, "Onibi.dmg")

        let fallback = try UpdaterController.decodeRelease(
            data: releaseJSON(
                tag: "v2.1.0",
                assets: [
                    ("Onibi.zip", "https://example.com/Onibi.zip"),
                    ("Onibi-2.1.0.dmg", "https://example.com/Onibi-2.1.0.dmg")
                ]
            )
        )
        XCTAssertEqual(fallback.asset.name, "Onibi-2.1.0.dmg")
    }

    func testManualCheckErrorSurfacesWarningToast() async throws {
        let defaults = makeDefaults()
        let networking = MockUpdaterNetworking(error: URLError(.notConnectedToInternet))
        let controller = UpdaterController(
            currentVersion: "2.0.0",
            networking: networking,
            defaults: defaults,
            downloadsDirectory: temporaryDownloadsDirectory()
        )

        controller.checkForUpdatesNow(trigger: .manual)
        try await waitFor { controller.toast != nil }

        XCTAssertEqual(controller.toast?.kind, .warning)
        XCTAssertTrue(controller.toast?.message.contains("Update check failed") == true)
    }

    func testAutomaticCheckRespectsThrottleAndDefaultEnabledPreference() async throws {
        let defaults = makeDefaults()
        let now = Date(timeIntervalSince1970: 1_700_000_000)
        defaults.set(now.addingTimeInterval(-60), forKey: UserDefaultsKeys.updatesLastCheckAt)

        let throttledNetworking = MockUpdaterNetworking(
            latestData: releaseJSON(tag: "v2.1.0", assetName: "Onibi.dmg")
        )
        let throttled = UpdaterController(
            currentVersion: "2.0.0",
            networking: throttledNetworking,
            defaults: defaults,
            downloadsDirectory: temporaryDownloadsDirectory(),
            now: { now }
        )
        XCTAssertTrue(throttled.automaticallyChecksForUpdates)

        throttled.performAutomaticCheckIfNeeded()
        try await Task.sleep(nanoseconds: 50_000_000)

        XCTAssertEqual(throttledNetworking.latestRequestCount, 0)

        let staleDefaults = makeDefaults()
        staleDefaults.set(now.addingTimeInterval(-90_000), forKey: UserDefaultsKeys.updatesLastCheckAt)
        let staleNetworking = MockUpdaterNetworking(
            latestData: releaseJSON(tag: "v2.1.0", assetName: "Onibi.dmg")
        )
        let stale = UpdaterController(
            currentVersion: "2.0.0",
            networking: staleNetworking,
            defaults: staleDefaults,
            downloadsDirectory: temporaryDownloadsDirectory(),
            now: { now }
        )

        stale.performAutomaticCheckIfNeeded()
        try await waitFor { staleNetworking.latestRequestCount == 1 }
    }

    func testDownloadDestinationSanitizesFilenameAndWritesToInjectedDownloadsDirectory() async throws {
        let defaults = makeDefaults()
        let downloadsDirectory = temporaryDownloadsDirectory()
        let networking = MockUpdaterNetworking(
            latestData: releaseJSON(tag: "v2.1.0", assetName: "Onibi/../../bad?.dmg")
        )
        let controller = UpdaterController(
            currentVersion: "2.0.0",
            networking: networking,
            defaults: defaults,
            downloadsDirectory: downloadsDirectory
        )

        controller.checkForUpdatesNow(trigger: .manual)
        try await waitFor {
            if case .downloaded = controller.downloadState {
                return true
            }
            return false
        }

        let downloadedURL = try XCTUnwrap(networking.lastDownloadDestination)
        XCTAssertEqual(downloadedURL.deletingLastPathComponent(), downloadsDirectory)
        XCTAssertEqual(downloadedURL.lastPathComponent, "Onibi-.-.-bad-.dmg")
        XCTAssertTrue(FileManager.default.fileExists(atPath: downloadedURL.path))
    }

    private func makeDefaults() -> UserDefaults {
        let suiteName = "UpdaterControllerTests.\(UUID().uuidString)"
        let defaults = UserDefaults(suiteName: suiteName)!
        defaults.removePersistentDomain(forName: suiteName)
        return defaults
    }

    private func temporaryDownloadsDirectory() -> URL {
        FileManager.default.temporaryDirectory
            .appendingPathComponent("OnibiUpdaterTests", isDirectory: true)
            .appendingPathComponent(UUID().uuidString, isDirectory: true)
    }

    private func waitFor(
        timeout: TimeInterval = 2.0,
        condition: @escaping @MainActor () -> Bool
    ) async throws {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if condition() {
                return
            }
            try await Task.sleep(nanoseconds: 20_000_000)
        }
        XCTFail("Timed out waiting for condition.")
    }

    private func releaseJSON(tag: String, assetName: String) -> Data {
        releaseJSON(tag: tag, assets: [(assetName, "https://example.com/\(assetName)")])
    }

    private func releaseJSON(tag: String, assets: [(String, String)]) -> Data {
        let assetJSON = assets.enumerated().map { index, asset in
            """
            {
              "id": \(index + 10),
              "name": "\(asset.0)",
              "browser_download_url": "\(asset.1)",
              "size": 2048
            }
            """
        }.joined(separator: ",")

        return """
        {
          "id": 123,
          "tag_name": "\(tag)",
          "name": "Onibi \(tag)",
          "body": "Release notes",
          "published_at": "2026-05-07T00:00:00Z",
          "html_url": "https://github.com/gongahkia/onibi/releases/tag/\(tag)",
          "assets": [\(assetJSON)]
        }
        """.data(using: .utf8)!
    }
}

private final class MockUpdaterNetworking: UpdaterNetworking {
    private let latestData: Data?
    private let error: Error?

    private(set) var latestRequestCount = 0
    private(set) var lastDownloadDestination: URL?

    init(latestData: Data? = nil, error: Error? = nil) {
        self.latestData = latestData
        self.error = error
    }

    func latestReleaseData(apiURL: URL, userAgent: String) async throws -> (Data, HTTPURLResponse) {
        latestRequestCount += 1
        if let error {
            throw error
        }
        let response = HTTPURLResponse(url: apiURL, statusCode: 200, httpVersion: nil, headerFields: nil)!
        return (latestData ?? Data(), response)
    }

    func downloadFile(
        from sourceURL: URL,
        to destinationURL: URL,
        userAgent: String,
        progress: @escaping @MainActor (Double?) -> Void
    ) async throws -> URL {
        lastDownloadDestination = destinationURL
        try FileManager.default.createDirectory(at: destinationURL.deletingLastPathComponent(), withIntermediateDirectories: true)
        try Data("dmg".utf8).write(to: destinationURL)
        await progress(1)
        return destinationURL
    }
}
