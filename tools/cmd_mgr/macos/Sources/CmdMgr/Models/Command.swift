import Foundation

/// Type of command: long-running service or one-shot execution.
enum CommandType: String, CaseIterable, Identifiable {
    case longRunning = "long-running"
    case oneShot = "one-shot"

    var id: String { rawValue }

    var displayName: String {
        switch self {
        case .longRunning: return "Long-running"
        case .oneShot: return "One-shot"
        }
    }
}

/// A saved command configuration.
struct Command: Identifiable, Equatable {
    let id: Int
    var name: String
    var command: String
    var cmdType: CommandType
    var workingDirectory: String?
    var createdAt: Date

    static func == (lhs: Command, rhs: Command) -> Bool {
        lhs.id == rhs.id
    }
}
