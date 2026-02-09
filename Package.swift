// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "GhosttyMenubar",
    platforms: [
        .macOS(.v13)
    ],
    products: [
        .executable(name: "GhosttyMenubar", targets: ["GhosttyMenubar"])
    ],
    dependencies: [
    ],
    targets: [
        .executableTarget(
            name: "GhosttyMenubar",
            dependencies: [],
            path: "GhosttyMenubar"
        )
    ]
)
