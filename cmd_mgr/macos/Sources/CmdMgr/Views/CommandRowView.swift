import SwiftUI

/// A single command row in the list.
struct CommandRowView: View {
    let command: Command
    @EnvironmentObject var appState: AppState

    @State private var showDeleteConfirm = false

    private var isRunning: Bool {
        appState.processManager.isRunning(command.id)
    }

    private var hasOutput: Bool {
        appState.processManager.hasOutput(command.id)
    }

    var body: some View {
        HStack(spacing: 12) {
            // Status indicator
            Circle()
                .fill(isRunning ? Color.green : Color.secondary.opacity(0.3))
                .frame(width: 10, height: 10)
                .shadow(color: isRunning ? .green.opacity(0.5) : .clear, radius: 4)

            // Name & command preview
            VStack(alignment: .leading, spacing: 2) {
                HStack(spacing: 8) {
                    Text(command.name)
                        .font(.system(size: 13, weight: .semibold))
                        .lineLimit(1)

                    Text(command.cmdType.displayName)
                        .font(.system(size: 10, weight: .medium))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 2)
                        .background(
                            command.cmdType == .longRunning
                                ? Color.blue.opacity(0.15)
                                : Color.orange.opacity(0.15)
                        )
                        .foregroundColor(
                            command.cmdType == .longRunning ? .blue : .orange
                        )
                        .cornerRadius(4)
                }

                Text(truncatedCommand)
                    .font(.system(size: 11, design: .monospaced))
                    .foregroundColor(.secondary)
                    .lineLimit(1)
            }

            Spacer()

            // Action buttons
            HStack(spacing: 6) {
                if command.cmdType == .longRunning {
                    // Start / Stop toggle
                    Button {
                        appState.toggleLongRunning(command)
                    } label: {
                        Image(systemName: isRunning ? "stop.fill" : "play.fill")
                            .foregroundColor(isRunning ? .red : .green)
                    }
                    .buttonStyle(.bordered)
                    .help(isRunning ? "Stop" : "Start")
                } else {
                    // Execute (disabled while already running)
                    Button {
                        appState.executeOneShot(command)
                    } label: {
                        Image(systemName: isRunning ? "hourglass" : "play.fill")
                            .foregroundColor(isRunning ? .orange : .green)
                    }
                    .buttonStyle(.bordered)
                    .disabled(isRunning)
                    .help(isRunning ? "Running…" : "Execute")
                }

                // View Output (enabled whenever output exists, running or not)
                Button {
                    appState.showOutput(command)
                } label: {
                    Image(systemName: "text.alignleft")
                }
                .buttonStyle(.bordered)
                .disabled(!hasOutput)
                .help("View Output")

                // History (both command types)
                Button {
                    appState.showHistory(command)
                } label: {
                    Image(systemName: "clock.arrow.circlepath")
                }
                .buttonStyle(.bordered)
                .help("View History")

                // Edit
                Button {
                    appState.openEditSheet(command)
                } label: {
                    Image(systemName: "pencil")
                }
                .buttonStyle(.bordered)
                .help("Edit")

                // Delete
                Button {
                    showDeleteConfirm = true
                } label: {
                    Image(systemName: "trash")
                        .foregroundColor(.red)
                }
                .buttonStyle(.bordered)
                .help("Delete")
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(
            RoundedRectangle(cornerRadius: 8)
                .fill(Color(nsColor: .controlBackgroundColor))
                .shadow(color: .black.opacity(0.05), radius: 2, y: 1)
        )
        .alert("Delete Command", isPresented: $showDeleteConfirm) {
            Button("Delete", role: .destructive) {
                appState.deleteCommand(command)
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("Are you sure you want to delete \"\(command.name)\"?")
        }
    }

    private var truncatedCommand: String {
        let cmd = command.command
        if cmd.count > 80 {
            return String(cmd.prefix(80)) + "…"
        }
        return cmd
    }
}
