// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "Onibi",
    platforms: [
        .macOS(.v13)
    ],
    products: [
        .executable(name: "Onibi", targets: ["Onibi"])
    ],
    dependencies: [
    ],
    targets: [
        .executableTarget(
            name: "Onibi",
            dependencies: [],
            path: "Onibi"
        ),
        .testTarget(
            name: "OnibiTests",
            dependencies: ["Onibi"],
            path: "OnibiTests"
        )
    ]
)
