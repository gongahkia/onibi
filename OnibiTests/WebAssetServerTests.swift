import XCTest
import OnibiCore
@testable import Onibi

final class WebAssetServerTests: XCTestCase {
    private var tempDirectoryURL: URL!

    override func setUpWithError() throws {
        let directory = FileManager.default.temporaryDirectory
            .appendingPathComponent("onibi-web-assets-\(UUID().uuidString)", isDirectory: true)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try Data("<html><body>Onibi Web</body></html>".utf8)
            .write(to: directory.appendingPathComponent("index.html"))
        try FileManager.default.createDirectory(
            at: directory.appendingPathComponent("assets", isDirectory: true),
            withIntermediateDirectories: true
        )
        try Data("console.log('ok');".utf8)
            .write(to: directory.appendingPathComponent("assets/main.js"))
        tempDirectoryURL = directory
    }

    override func tearDownWithError() throws {
        if let tempDirectoryURL {
            try? FileManager.default.removeItem(at: tempDirectoryURL)
        }
    }

    func testSkipsAPIPaths() {
        let server = WebAssetServer(candidateDirectories: [tempDirectoryURL])
        let response = server.response(method: "GET", path: "/api/v2/bootstrap")
        XCTAssertNil(response)
    }

    func testServesIndexAtRoot() throws {
        let server = WebAssetServer(candidateDirectories: [tempDirectoryURL])
        guard let response = server.response(method: "GET", path: "/") else {
            XCTFail("expected response")
            return
        }

        XCTAssertEqual(response.statusCode, 200)
        XCTAssertEqual(response.headers["Content-Type"], "text/html; charset=utf-8")
        XCTAssertEqual(String(data: response.body, encoding: .utf8), "<html><body>Onibi Web</body></html>")
    }

    func testServesStaticAsset() throws {
        let server = WebAssetServer(candidateDirectories: [tempDirectoryURL])
        guard let response = server.response(method: "GET", path: "/assets/main.js") else {
            XCTFail("expected response")
            return
        }

        XCTAssertEqual(response.statusCode, 200)
        XCTAssertEqual(response.headers["Content-Type"], "application/javascript; charset=utf-8")
        XCTAssertEqual(response.headers["Cache-Control"], "public, max-age=31536000, immutable")
        XCTAssertEqual(String(data: response.body, encoding: .utf8), "console.log('ok');")
    }

    func testServesSPAFallbackForSessionRoute() {
        let server = WebAssetServer(candidateDirectories: [tempDirectoryURL])
        guard let response = server.response(method: "GET", path: "/sessions/abc-123") else {
            XCTFail("expected response")
            return
        }

        XCTAssertEqual(response.statusCode, 200)
        XCTAssertEqual(response.headers["Content-Type"], "text/html; charset=utf-8")
    }

    func testRejectsDirectoryTraversal() {
        let server = WebAssetServer(candidateDirectories: [tempDirectoryURL])
        guard let response = server.response(method: "GET", path: "/../../etc/passwd") else {
            XCTFail("expected response")
            return
        }

        XCTAssertEqual(response.statusCode, 404)
    }
}
