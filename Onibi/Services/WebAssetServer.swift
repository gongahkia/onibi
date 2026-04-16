import Foundation
import OnibiCore

struct WebAssetServer {
    private let fileManager: FileManager
    private let candidateDirectories: [URL]

    init(
        fileManager: FileManager = .default,
        candidateDirectories: [URL] = WebAssetServer.defaultCandidateDirectories()
    ) {
        self.fileManager = fileManager
        self.candidateDirectories = candidateDirectories
    }

    func response(method: String, path: String) -> MobileGatewayResponse? {
        guard shouldHandle(path: path) else {
            return nil
        }

        guard method.uppercased() == "GET" else {
            return textResponse(
                statusCode: 405,
                body: "Method not allowed",
                contentType: "text/plain; charset=utf-8"
            )
        }

        guard let root = resolveWebRoot() else {
            return textResponse(
                statusCode: 503,
                body: "OnibiWeb assets unavailable. Build OnibiWeb and place dist/ under OnibiWeb.",
                contentType: "text/plain; charset=utf-8"
            )
        }

        guard let fileURL = resolveFileURL(path: path, root: root) else {
            return textResponse(
                statusCode: 404,
                body: "Not found",
                contentType: "text/plain; charset=utf-8"
            )
        }

        do {
            let data = try Data(contentsOf: fileURL)
            let contentType = mimeType(for: fileURL.pathExtension)
            let cacheControl = cachePolicy(path: path, contentType: contentType)
            return MobileGatewayResponse(
                statusCode: 200,
                headers: [
                    "Content-Type": contentType,
                    "Content-Length": "\(data.count)",
                    "Cache-Control": cacheControl
                ],
                body: data
            )
        } catch {
            return textResponse(
                statusCode: 500,
                body: "Failed to load web asset",
                contentType: "text/plain; charset=utf-8"
            )
        }
    }

    private func shouldHandle(path: String) -> Bool {
        !path.hasPrefix("/api/")
    }

    private func resolveWebRoot() -> URL? {
        for candidate in candidateDirectories {
            var isDirectory: ObjCBool = false
            if fileManager.fileExists(atPath: candidate.path, isDirectory: &isDirectory), isDirectory.boolValue {
                let indexURL = candidate.appendingPathComponent("index.html")
                if fileManager.fileExists(atPath: indexURL.path) {
                    return candidate
                }
            }
        }
        return nil
    }

    private func resolveFileURL(path: String, root: URL) -> URL? {
        let decodedPath = path.removingPercentEncoding ?? path
        let normalizedPath = decodedPath.isEmpty ? "/" : decodedPath

        if normalizedPath == "/" {
            return root.appendingPathComponent("index.html")
        }

        let relativePath = normalizedPath
            .trimmingCharacters(in: CharacterSet(charactersIn: "/"))
            .replacingOccurrences(of: "//", with: "/")

        guard !relativePath.contains("..") else {
            return nil
        }

        let candidate = root.appendingPathComponent(relativePath).standardizedFileURL
        let rootPath = root.standardizedFileURL.path
        guard candidate.path.hasPrefix(rootPath + "/") || candidate.path == rootPath else {
            return nil
        }

        var isDirectory: ObjCBool = false
        if fileManager.fileExists(atPath: candidate.path, isDirectory: &isDirectory), !isDirectory.boolValue {
            return candidate
        }

        if shouldServeSPAFallback(for: normalizedPath) {
            return root.appendingPathComponent("index.html")
        }

        return nil
    }

    private func shouldServeSPAFallback(for path: String) -> Bool {
        let knownRoutes = ["/connect", "/sessions"]
        if knownRoutes.contains(path) || path.hasPrefix("/sessions/") {
            return true
        }

        let extensionValue = URL(fileURLWithPath: path).pathExtension
        return extensionValue.isEmpty
    }

    private func mimeType(for pathExtension: String) -> String {
        switch pathExtension.lowercased() {
        case "html":
            return "text/html; charset=utf-8"
        case "js", "mjs":
            return "application/javascript; charset=utf-8"
        case "css":
            return "text/css; charset=utf-8"
        case "json", "map":
            return "application/json; charset=utf-8"
        case "svg":
            return "image/svg+xml"
        case "png":
            return "image/png"
        case "jpg", "jpeg":
            return "image/jpeg"
        case "ico":
            return "image/x-icon"
        case "woff":
            return "font/woff"
        case "woff2":
            return "font/woff2"
        default:
            return "application/octet-stream"
        }
    }

    private func cachePolicy(path: String, contentType: String) -> String {
        if path.hasPrefix("/assets/") && !contentType.hasPrefix("text/html") {
            return "public, max-age=31536000, immutable"
        }
        return "no-cache"
    }

    private func textResponse(statusCode: Int, body: String, contentType: String) -> MobileGatewayResponse {
        let data = Data(body.utf8)
        return MobileGatewayResponse(
            statusCode: statusCode,
            headers: [
                "Content-Type": contentType,
                "Content-Length": "\(data.count)"
            ],
            body: data
        )
    }
}

extension WebAssetServer {
    static func defaultCandidateDirectories(
        environment: [String: String] = ProcessInfo.processInfo.environment,
        currentDirectoryPath: String = FileManager.default.currentDirectoryPath,
        executableURL: URL? = Bundle.main.executableURL,
        resourceURL: URL? = Bundle.main.resourceURL
    ) -> [URL] {
        var candidates: [URL] = []

        if let bundledRoot = bundledWebRoot() {
            candidates.append(bundledRoot)
        }

        if let explicit = environment["ONIBI_WEB_DIST_DIR"], !explicit.isEmpty {
            candidates.append(URL(fileURLWithPath: explicit, isDirectory: true))
        }

        let currentDirectory = URL(fileURLWithPath: currentDirectoryPath, isDirectory: true)
        candidates.append(currentDirectory.appendingPathComponent("OnibiWeb/dist", isDirectory: true))
        candidates.append(currentDirectory.appendingPathComponent("dist", isDirectory: true))

        if let executableURL {
            let executableDirectory = executableURL.deletingLastPathComponent()
            candidates.append(executableDirectory.appendingPathComponent("OnibiWeb/dist", isDirectory: true))
            candidates.append(executableDirectory.appendingPathComponent("dist", isDirectory: true))
        }

        if let resourceURL {
            candidates.append(resourceURL.appendingPathComponent("OnibiWeb/dist", isDirectory: true))
            candidates.append(resourceURL.appendingPathComponent("OnibiWeb", isDirectory: true))
            candidates.append(resourceURL.appendingPathComponent("dist", isDirectory: true))
        }

        var seen = Set<String>()
        return candidates.filter { candidate in
            let standardizedPath = candidate.standardizedFileURL.path
            guard !standardizedPath.isEmpty else {
                return false
            }
            let inserted = seen.insert(standardizedPath).inserted
            return inserted
        }
    }

    private static func bundledWebRoot() -> URL? {
        let resourceURL = Bundle.module.resourceURL
        if let directPath = resourceURL?.appendingPathComponent("OnibiWeb", isDirectory: true) {
            return directPath
        }
        return nil
    }
}
