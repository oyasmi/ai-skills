import SwiftUI

struct ContentView: View {
    @EnvironmentObject var appState: AppState

    var body: some View {
        NavigationSplitView {
            sidebar
                .navigationSplitViewColumnWidth(min: 230, ideal: 290, max: 380)
        } detail: {
            VStack(spacing: 0) {
                if let message = appState.errorMessage {
                    ErrorBanner(message: message, dismiss: appState.dismissError)
                }

                if let command = appState.selectedCommand {
                    CommandDetailView(command: command)
                        .environmentObject(appState)
                } else {
                    EmptySelectionView(hasCommands: !appState.commands.isEmpty) {
                        appState.openAddSheet()
                    }
                }
            }
        }
        .searchable(text: $appState.searchText, placement: .sidebar,
                    prompt: "Search commands")
        .toolbar {
            ToolbarItem {
                Picker("Filter", selection: $appState.commandFilter) {
                    ForEach(CommandFilter.allCases) { filter in
                        Text(filter.displayName).tag(filter)
                    }
                }
                .labelsHidden()
                .frame(width: 125)
                .help("Filter commands")
            }

            ToolbarItem(placement: .primaryAction) {
                Button {
                    appState.openAddSheet()
                } label: {
                    Label("New Command", systemImage: "plus")
                }
                .keyboardShortcut("n", modifiers: .command)
            }
        }
        .sheet(isPresented: $appState.showAddEditSheet) {
            AddEditCommandSheet(command: appState.editingCommand)
                .environmentObject(appState)
        }
    }

    private var sidebar: some View {
        Group {
            if appState.filteredCommands.isEmpty {
                VStack(spacing: 10) {
                    Spacer()
                    Image(systemName: appState.commands.isEmpty ? "terminal" : "line.3.horizontal.decrease.circle")
                        .font(.system(size: 32))
                        .foregroundStyle(.secondary)
                    Text(appState.commands.isEmpty ? "No Commands" : "No Matches")
                        .font(.headline)
                    Text(appState.commands.isEmpty
                         ? "Create a command to get started."
                         : "Change the search or filter.")
                        .font(.subheadline)
                        .foregroundStyle(.secondary)
                        .multilineTextAlignment(.center)
                    if appState.commands.isEmpty {
                        Button("New Command") { appState.openAddSheet() }
                            .buttonStyle(.borderedProminent)
                    }
                    Spacer()
                }
                .padding()
            } else {
                List(appState.filteredCommands, selection: $appState.selectedCommandID) { command in
                    CommandRowView(command: command)
                        .environmentObject(appState)
                        .tag(command.id)
                }
                .listStyle(.sidebar)
                .navigationTitle("Commands")
            }
        }
    }
}

private struct ErrorBanner: View {
    let message: String
    let dismiss: () -> Void

    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: "exclamationmark.triangle.fill")
                .foregroundStyle(.red)
            Text(message)
                .font(.subheadline)
                .lineLimit(2)
            Spacer()
            Button(action: dismiss) {
                Image(systemName: "xmark")
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Dismiss error")
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 9)
        .background(Color.red.opacity(0.1))
        .overlay(alignment: .bottom) { Divider() }
    }
}

private struct EmptySelectionView: View {
    let hasCommands: Bool
    let create: () -> Void

    var body: some View {
        VStack(spacing: 12) {
            Image(systemName: "terminal")
                .font(.system(size: 44))
                .foregroundStyle(.secondary)
            Text(hasCommands ? "Select a Command" : "Create Your First Command")
                .font(.title2.weight(.semibold))
            Text(hasCommands
                 ? "Choose a command from the sidebar to view its output and history."
                 : "Save frequently used shell commands and run them without leaving the app.")
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
                .frame(maxWidth: 390)
            if !hasCommands {
                Button("New Command", action: create)
                    .buttonStyle(.borderedProminent)
                    .controlSize(.large)
            }
        }
        .padding(30)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
