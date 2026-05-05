// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "CmdMgr",
    platforms: [.macOS(.v13)],
    targets: [
        .systemLibrary(
            name: "CSQLite",
            path: "Sources/CSQLite"
        ),
        .executableTarget(
            name: "CmdMgr",
            dependencies: ["CSQLite"],
            path: "Sources/CmdMgr"
        )
    ]
)
