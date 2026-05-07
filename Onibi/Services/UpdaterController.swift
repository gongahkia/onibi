import AppKit
import Combine
import Foundation

struct AvailableRelease: Equatable, Identifiable {
    struct Asset: Equatable {
        let name: String
        let downloadURL: URL
        let size: Int64?
    }

    let id: Int
    let version: String
    let tagName: String
    let title: String
    let notes: String
    let publishedAt: Date
    let htmlURL: URL
    let asset: Asset
}

enum DownloadState: Equatable {
    case idle
    case downloading(progress: Double?)
    case downloaded(URL)
    case failed(String)
}

struct ToastState: Equatable, Identifiable {
    enum Kind: Equatable {
        case info
        case success
        case warning
    }

    let id = UUID()
    let kind: Kind
    let message: String

    static func == (lhs: ToastState, rhs: ToastState) -> Bool {
        lhs.id == rhs.id && lhs.kind == rhs.kind && lhs.message == rhs.message
    }
}

enum CheckTrigger {
    case manual
    case automatic
}

protocol UpdaterNetworking {
    func latestReleaseData(apiURL: URL, userAgent: String) async throws -> (Data, HTTPURLResponse)
    func downloadFile(from sourceURL: URL, to destinationURL: URL, userAgent: String, progress: @escaping @MainActor (Double?) -> Void) async throws -> URL
}

struct URLSessionUpdaterNetworking: UpdaterNetworking {
    func latestReleaseData(apiURL: URL, userAgent: String) async throws -> (Data, HTTPURLResponse) {
        var request = URLRequest(url: apiURL)
        request.setValue(userAgent, forHTTPHeaderField: "User-Agent")
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")

        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw UpdaterError.invalidHTTPResponse
        }
        return (data, http)
    }

    func downloadFile(
        from sourceURL: URL,
        to destinationURL: URL,
        userAgent: String,
        progress: @escaping @MainActor (Double?) -> Void
    ) async throws -> URL {
        var request = URLRequest(url: sourceURL)
        request.setValue(userAgent, forHTTPHeaderField: "User-Agent")

        let (bytes, response) = try await URLSession.shared.bytes(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw UpdaterError.invalidHTTPResponse
        }
        guard (200..<300).contains(http.statusCode) else {
            throw UpdaterError.badStatus(http.statusCode)
        }

        let fileManager = FileManager.default
        try fileManager.createDirectory(at: destinationURL.deletingLastPathComponent(), withIntermediateDirectories: true)

        let temporaryURL = destinationURL
            .deletingLastPathComponent()
            .appendingPathComponent(".\(destinationURL.lastPathComponent).download", isDirectory: false)
        try? fileManager.removeItem(at: temporaryURL)

        guard fileManager.createFile(atPath: temporaryURL.path, contents: nil),
              let handle = try? FileHandle(forWritingTo: temporaryURL)
        else {
            throw UpdaterError.cannotCreateDownloadFile
        }

        var received: Int64 = 0
        let expected = response.expectedContentLength > 0 ? response.expectedContentLength : -1

        do {
            for try await byte in bytes {
                try handle.write(contentsOf: [byte])
                received += 1
                if expected > 0 && received % 65_536 == 0 {
                    let fraction = min(1, Double(received) / Double(expected))
                    await progress(fraction)
                }
            }
            try handle.close()
        } catch {
            try? handle.close()
            try? fileManager.removeItem(at: temporaryURL)
            throw error
        }

        try? fileManager.removeItem(at: destinationURL)
        try fileManager.moveItem(at: temporaryURL, to: destinationURL)
        await progress(1)
        return destinationURL
    }
}

enum UpdaterError: LocalizedError, Equatable {
    case invalidHTTPResponse
    case badStatus(Int)
    case noDMGAsset
    case cannotCreateDownloadFile

    var errorDescription: String? {
        switch self {
        case .invalidHTTPResponse:
            return "GitHub returned a non-HTTP response."
        case .badStatus(let status):
            return "GitHub returned HTTP \(status)."
        case .noDMGAsset:
            return "The latest release does not include a DMG download."
        case .cannotCreateDownloadFile:
            return "Onibi could not create the download file."
        }
    }
}

@MainActor
final class UpdaterController: ObservableObject {
    static let shared = UpdaterController()

    @Published var automaticallyChecksForUpdates: Bool {
        didSet {
            defaults.set(automaticallyChecksForUpdates, forKey: UserDefaultsKeys.updatesAutoCheckEnabled)
        }
    }
    @Published private(set) var isChecking = false
    @Published private(set) var lastCheckAt: Date?
    @Published private(set) var availableRelease: AvailableRelease?
    @Published private(set) var downloadState: DownloadState = .idle
    @Published var toast: ToastState?

    private let latestReleaseAPIURL: URL
    private let releasesPageURL: URL
    private let preferredAssetName = "Onibi.dmg"
    private let currentVersion: String
    private let networking: UpdaterNetworking
    private let defaults: UserDefaults
    private let downloadsDirectory: URL
    private let fileManager: FileManager
    private let now: () -> Date
    private let checkInterval: TimeInterval = 24 * 60 * 60

    init(
        currentVersion: String = UpdaterController.bundleVersion,
        latestReleaseAPIURL: URL = URL(string: "https://api.github.com/repos/gongahkia/onibi/releases/latest")!,
        releasesPageURL: URL = URL(string: "https://github.com/gongahkia/onibi/releases")!,
        networking: UpdaterNetworking = URLSessionUpdaterNetworking(),
        defaults: UserDefaults = .standard,
        downloadsDirectory: URL? = nil,
        fileManager: FileManager = .default,
        now: @escaping () -> Date = Date.init
    ) {
        self.currentVersion = currentVersion
        self.latestReleaseAPIURL = latestReleaseAPIURL
        self.releasesPageURL = releasesPageURL
        self.networking = networking
        self.defaults = defaults
        self.fileManager = fileManager
        self.now = now

        defaults.register(defaults: [UserDefaultsKeys.updatesAutoCheckEnabled: true])
        self.automaticallyChecksForUpdates = defaults.object(forKey: UserDefaultsKeys.updatesAutoCheckEnabled) as? Bool ?? true
        self.lastCheckAt = defaults.object(forKey: UserDefaultsKeys.updatesLastCheckAt) as? Date

        if let downloadsDirectory {
            self.downloadsDirectory = downloadsDirectory
        } else {
            self.downloadsDirectory = fileManager.urls(for: .downloadsDirectory, in: .userDomainMask).first
                ?? URL(fileURLWithPath: NSHomeDirectory()).appendingPathComponent("Downloads", isDirectory: true)
        }
    }

    func checkForUpdates() {
        checkForUpdatesNow(trigger: .manual)
    }

    func checkForUpdatesNow(trigger: CheckTrigger) {
        guard !isChecking else { return }

        Task {
            await runCheck(trigger: trigger)
        }
    }

    func performAutomaticCheckIfNeeded() {
        guard automaticallyChecksForUpdates else { return }
        if let lastCheckAt, now().timeIntervalSince(lastCheckAt) < checkInterval {
            return
        }
        checkForUpdatesNow(trigger: .automatic)
    }

    func openAvailableReleaseDownload() {
        guard let release = availableRelease else { return }
        if case .downloaded(let url) = downloadState {
            NSWorkspace.shared.open(url)
        } else {
            download(release)
        }
    }

    func retryAvailableReleaseDownload() {
        guard let release = availableRelease else { return }
        download(release)
    }

    func revealDownloadedReleaseInFinder() {
        guard case .downloaded(let url) = downloadState else { return }
        NSWorkspace.shared.activateFileViewerSelecting([url])
    }

    func revealCurrentAppInFinder() {
        NSWorkspace.shared.activateFileViewerSelecting([Bundle.main.bundleURL])
    }

    func quitForUpdateInstall() {
        NSApplication.shared.terminate(nil)
    }

    func openReleasesPage() {
        NSWorkspace.shared.open(releasesPageURL)
    }

    private func runCheck(trigger: CheckTrigger) async {
        isChecking = true
        defer { isChecking = false }

        let checkedAt = now()
        lastCheckAt = checkedAt
        defaults.set(checkedAt, forKey: UserDefaultsKeys.updatesLastCheckAt)

        do {
            let (data, response) = try await networking.latestReleaseData(apiURL: latestReleaseAPIURL, userAgent: userAgent)
            guard (200..<300).contains(response.statusCode) else {
                throw UpdaterError.badStatus(response.statusCode)
            }

            let release = try Self.decodeRelease(data: data, preferredAssetName: preferredAssetName)
            guard Self.isVersion(release.version, newerThan: currentVersion) else {
                availableRelease = nil
                downloadState = .idle
                if trigger == .manual {
                    toast = ToastState(kind: .success, message: "Onibi is up to date.")
                }
                return
            }

            availableRelease = release
            toast = ToastState(kind: .info, message: "Onibi \(release.version) is available.")
            download(release)
        } catch {
            if trigger == .manual {
                toast = ToastState(kind: .warning, message: "Update check failed: \(error.localizedDescription)")
            }
        }
    }

    private func download(_ release: AvailableRelease) {
        downloadState = .downloading(progress: nil)

        Task {
            do {
                let destination = downloadDestination(for: release.asset.name)
                let url = try await networking.downloadFile(from: release.asset.downloadURL, to: destination, userAgent: userAgent) { [weak self] progress in
                    self?.downloadState = .downloading(progress: progress)
                }
                downloadState = .downloaded(url)
                toast = ToastState(kind: .success, message: "Downloaded \(url.lastPathComponent).")
            } catch {
                downloadState = .failed(error.localizedDescription)
                toast = ToastState(kind: .warning, message: "Download failed: \(error.localizedDescription)")
            }
        }
    }

    private var userAgent: String {
        "Onibi/\(currentVersion)"
    }

    private func downloadDestination(for assetName: String) -> URL {
        downloadsDirectory.appendingPathComponent(Self.sanitizedFilename(assetName, fallback: preferredAssetName), isDirectory: false)
    }

    nonisolated static var bundleVersion: String {
        Bundle.main.infoDictionary?["CFBundleShortVersionString"] as? String ?? "dev"
    }

    static func decodeRelease(data: Data, preferredAssetName: String = "Onibi.dmg") throws -> AvailableRelease {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        let response = try decoder.decode(GitHubReleaseResponse.self, from: data)

        let preferredAsset = response.assets.first { $0.name == preferredAssetName }
        let fallbackAsset = response.assets.first { $0.name.lowercased().hasSuffix(".dmg") }
        guard let asset = preferredAsset ?? fallbackAsset,
              let downloadURL = URL(string: asset.browserDownloadURL),
              let htmlURL = URL(string: response.htmlURL)
        else {
            throw UpdaterError.noDMGAsset
        }

        let version = normalizedVersion(response.tagName)
        return AvailableRelease(
            id: response.id,
            version: version,
            tagName: response.tagName,
            title: response.name?.isEmpty == false ? response.name! : response.tagName,
            notes: response.body?.trimmingCharacters(in: .whitespacesAndNewlines) ?? "",
            publishedAt: response.publishedAt,
            htmlURL: htmlURL,
            asset: AvailableRelease.Asset(name: asset.name, downloadURL: downloadURL, size: asset.size)
        )
    }

    static func isVersion(_ candidate: String, newerThan current: String) -> Bool {
        compareVersions(candidate, current) == .orderedDescending
    }

    static func compareVersions(_ lhs: String, _ rhs: String) -> ComparisonResult {
        let left = versionComponents(lhs)
        let right = versionComponents(rhs)
        let count = max(left.count, right.count)

        for index in 0..<count {
            let leftValue = index < left.count ? left[index] : 0
            let rightValue = index < right.count ? right[index] : 0
            if leftValue > rightValue { return .orderedDescending }
            if leftValue < rightValue { return .orderedAscending }
        }
        return .orderedSame
    }

    static func sanitizedFilename(_ filename: String, fallback: String = "Onibi.dmg") -> String {
        let trimmed = filename.trimmingCharacters(in: .whitespacesAndNewlines)
        let candidate = trimmed.isEmpty ? fallback : trimmed
        let allowed = CharacterSet(charactersIn: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789.-_ ")
        let scalars = candidate.unicodeScalars.map { allowed.contains($0) ? Character($0) : "-" }
        let sanitized = String(scalars).replacingOccurrences(of: "..", with: ".")
        return sanitized.hasSuffix(".dmg") ? sanitized : fallback
    }

    private static func normalizedVersion(_ version: String) -> String {
        let trimmed = version.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.lowercased().hasPrefix("v") {
            return String(trimmed.dropFirst())
        }
        return trimmed
    }

    private static func versionComponents(_ version: String) -> [Int] {
        normalizedVersion(version)
            .split(separator: "-", maxSplits: 1, omittingEmptySubsequences: true)
            .first?
            .split(separator: ".")
            .map { component in
                let prefix = component.prefix { $0.isNumber }
                return Int(prefix) ?? 0
            } ?? [0]
    }
}

private struct GitHubReleaseResponse: Decodable {
    let id: Int
    let tagName: String
    let name: String?
    let body: String?
    let publishedAt: Date
    let htmlURL: String
    let assets: [GitHubReleaseAsset]

    enum CodingKeys: String, CodingKey {
        case id
        case tagName = "tag_name"
        case name
        case body
        case publishedAt = "published_at"
        case htmlURL = "html_url"
        case assets
    }
}

private struct GitHubReleaseAsset: Decodable {
    let name: String
    let browserDownloadURL: String
    let size: Int64?

    enum CodingKeys: String, CodingKey {
        case name
        case browserDownloadURL = "browser_download_url"
        case size
    }
}
