import Foundation

/// A record of a command execution.
struct CommandHistory: Identifiable {
    let id: Int
    let commandId: Int
    let startTime: Date
    var endTime: Date?
    var status: String
    var output: String?
}
