import SwiftUI

extension ProcessStatus {
    var color: Color {
        switch self {
        case .running: return .blue
        case .success: return .green
        case .failed: return .red
        case .stopped: return .orange
        }
    }

    var systemImage: String {
        switch self {
        case .running: return "waveform.path"
        case .success: return "checkmark.circle.fill"
        case .failed: return "xmark.circle.fill"
        case .stopped: return "stop.circle.fill"
        }
    }
}

/// Compact sidebar representation of a saved command.
struct CommandRowView: View {
    let command: Command
    @EnvironmentObject var appState: AppState
    @State private var showDeleteConfirm = false

    private var processInfo: CommandProcess? {
        appState.processManager.processInfo(command.id)
    }

    var body: some View {
        HStack(spacing: 10) {
            statusIndicator

            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Text(command.name)
                        .font(.headline)
                        .lineLimit(1)
                    Spacer(minLength: 4)
                    if command.cmdType == .longRunning {
                        Image(systemName: "server.rack")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                            .help("Long-running service")
                    }
                }

                Text(command.command.replacingOccurrences(of: "\n", with: " "))
                    .font(.system(.caption, design: .monospaced))
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
        }
        .padding(.vertical, 5)
        .contentShape(Rectangle())
        .contextMenu {
            Button(processInfo?.isRunning == true ? "Stop" : "Run") {
                appState.run(command)
            }
            Button("Show Output") { appState.showOutput(command) }
            Button("Show History") { appState.showHistory(command) }
            Divider()
            Button("Edit…") { appState.openEditSheet(command) }
            Button("Duplicate") { appState.duplicateCommand(command) }
            Divider()
            Button("Delete…", role: .destructive) { showDeleteConfirm = true }
        }
        .alert("Delete Command?", isPresented: $showDeleteConfirm) {
            Button("Delete", role: .destructive) { appState.deleteCommand(command) }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(deleteMessage)
        }
    }

    @ViewBuilder
    private var statusIndicator: some View {
        if let status = processInfo?.status {
            Image(systemName: status.systemImage)
                .foregroundStyle(status.color)
                .frame(width: 16)
                .accessibilityLabel(status.displayName)
        } else {
            Circle()
                .fill(.tertiary)
                .frame(width: 7, height: 7)
                .frame(width: 16)
                .accessibilityLabel("Not run")
        }
    }

    private var deleteMessage: String {
        if processInfo?.isRunning == true {
            return "\"\(command.name)\" is running. Deleting it will stop the process and remove its execution history."
        }
        return "Deleting \"\(command.name)\" also removes its execution history. This cannot be undone."
    }
}
