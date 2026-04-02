// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "Onibi",
    platforms: [
        .macOS(.v14),
        .iOS(.v17)
    ],
    products: [
        .library(name: "OnibiCore", targets: ["OnibiCore"]),
        .executable(name: "Onibi", targets: ["Onibi"]),
        .executable(name: "OnibiSessionProxy", targets: ["OnibiSessionProxy"])
    ],
    dependencies: [
    ],
    targets: [
        .target(
            name: "OnibiCore",
            dependencies: [],
            path: "OnibiCore"
        ),
        .executableTarget(
            name: "Onibi",
            dependencies: ["OnibiCore"],
            path: "Onibi",
            exclude: [
                "Info.plist",
                "Assets.xcassets",
                "Onibi.entitlements"
            ]
        ),
        .executableTarget(
            name: "OnibiSessionProxy",
            dependencies: ["OnibiCore"],
            path: "OnibiSessionProxy"
        ),
        .testTarget(
            name: "OnibiTests",
            dependencies: ["Onibi", "OnibiCore"],
            path: "OnibiTests"
        ),
        .testTarget(
            name: "OnibiCoreTests",
            dependencies: ["OnibiCore"],
            path: "OnibiCoreTests"
        ),
        .testTarget(
            name: "OnibiSessionProxyTests",
            dependencies: ["OnibiSessionProxy", "OnibiCore"],
            path: "OnibiSessionProxyTests"
        )
    ]
)
