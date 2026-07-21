import SwiftUI

struct CommandDetailView: View {
    let command: Command
    @EnvironmentObject var appState: AppState
    @State private var showDeleteConfirm = false

    private var processInfo: CommandProcess? {
        appState.processManager.processInfo(command.id)
    }

    private var isRunning: Bool { processInfo?.isRunning == true }

    var body: some View {
        VStack(spacing: 0) {
            header

            Picker("Section", selection: $appState.selectedDetailTab) {
                ForEach(CommandDetailTab.allCases) { tab in
                    Text(tab.displayName).tag(tab)
                }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .frame(maxWidth: 380)
            .padding(.horizontal, 24)
            .padding(.bottom, 14)

            Divider()

            switch appState.selectedDetailTab {
            case .overview:
                CommandOverviewView(command: command)
            case .output:
                OutputView(processInfo: processInfo) {
                    appState.run(command)
                }
            case .history:
                HistoryView(history: appState.history(for: command)) {
                    appState.run(command)
                }
            }
        }
        .navigationTitle(command.name)
        .alert("Delete Command?", isPresented: $showDeleteConfirm) {
            Button("Delete", role: .destructive) { appState.deleteCommand(command) }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(deleteMessage)
        }
    }

    private var header: some View {
        HStack(alignment: .center, spacing: 14) {
            Image(systemName: command.cmdType == .longRunning ? "server.rack" : "terminal")
                .font(.system(size: 22, weight: .medium))
                .foregroundStyle(.tint)
                .frame(width: 42, height: 42)
                .background(.tint.opacity(0.1), in: RoundedRectangle(cornerRadius: 10))

            VStack(alignment: .leading, spacing: 4) {
                Text(command.name)
                    .font(.title2.weight(.semibold))
                    .lineLimit(1)
                HStack(spacing: 8) {
                    Text(command.cmdType.displayName)
                    if let status = processInfo?.status {
                        Label(status.displayName, systemImage: status.systemImage)
                            .foregroundStyle(status.color)
                    } else {
                        Label("Not run", systemImage: "circle")
                    }
                }
                .font(.subheadline)
                .foregroundStyle(.secondary)
            }

            Spacer()

            Button {
                appState.run(command)
            } label: {
                Label(primaryActionTitle,
                      systemImage: isRunning && command.cmdType == .longRunning
                        ? "stop.fill" : "play.fill")
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .tint(isRunning && command.cmdType == .longRunning ? .red : .accentColor)
            .disabled(isRunning && command.cmdType == .oneShot)

            Menu {
                Button("Edit…") { appState.openEditSheet(command) }
                Button("Duplicate") { appState.duplicateCommand(command) }
                Divider()
                Button("Delete…", role: .destructive) { showDeleteConfirm = true }
            } label: {
                Image(systemName: "ellipsis.circle")
            }
            .menuStyle(.borderlessButton)
            .menuIndicator(.hidden)
            .fixedSize()
            .help("More actions")
        }
        .padding(.horizontal, 24)
        .padding(.vertical, 18)
    }

    private var primaryActionTitle: String {
        if isRunning {
            return command.cmdType == .longRunning ? "Stop" : "Running…"
        }
        return processInfo == nil ? "Run" : "Run Again"
    }

    private var deleteMessage: String {
        if isRunning {
            return "This command is running. Deleting it will stop the process and remove all execution history."
        }
        return "This also removes all execution history and cannot be undone."
    }
}

private struct CommandOverviewView: View {
    let command: Command

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 22) {
                DetailSection(title: "Command") {
                    Text(command.command)
                        .font(.system(.body, design: .monospaced))
                        .textSelection(.enabled)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(12)
                        .background(.background, in: RoundedRectangle(cornerRadius: 8))
                }

                DetailSection(title: "Configuration") {
                    LabeledContent("Type", value: command.cmdType.displayName)
                    Divider()
                    LabeledContent("Working directory",
                                   value: command.workingDirectory ?? "Application default")
                    Divider()
                    LabeledContent("Shell", value: "/bin/sh")
                }
            }
            .padding(24)
            .frame(maxWidth: 760)
            .frame(maxWidth: .infinity)
        }
        .background(Color(nsColor: .windowBackgroundColor))
    }
}

private struct DetailSection<Content: View>: View {
    let title: String
    @ViewBuilder let content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title)
                .font(.headline)
            VStack(alignment: .leading, spacing: 10) {
                content
            }
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Color(nsColor: .controlBackgroundColor),
                        in: RoundedRectangle(cornerRadius: 10))
        }
    }
}
