import SwiftUI

/// Main content view – a list of commands with a toolbar and search.
struct ContentView: View {
    @EnvironmentObject var appState: AppState

    var body: some View {
        VStack(spacing: 0) {
            // Header
            HStack {
                Text("Command Manager")
                    .font(.title2)
                    .fontWeight(.bold)
                Spacer()
                Button {
                    appState.openAddSheet()
                } label: {
                    Label("Add Command", systemImage: "plus.circle.fill")
                }
                .buttonStyle(.borderedProminent)
                .controlSize(.regular)
                .keyboardShortcut("n", modifiers: .command)
            }
            .padding(.horizontal, 20)
            .padding(.vertical, 14)

            // Search bar
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass")
                    .foregroundColor(.secondary)
                TextField("Search commands…", text: $appState.searchText)
                    .textFieldStyle(.plain)
                if !appState.searchText.isEmpty {
                    Button {
                        appState.searchText = ""
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .foregroundColor(.secondary)
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 7)
            .background(Color(nsColor: .controlBackgroundColor))
            .cornerRadius(8)
            .padding(.horizontal, 20)
            .padding(.bottom, 10)

            Divider()

            // Command list or empty state
            if appState.filteredCommands.isEmpty {
                Spacer()
                VStack(spacing: 12) {
                    Image(systemName: appState.searchText.isEmpty ? "terminal" : "magnifyingglass")
                        .font(.system(size: 48))
                        .foregroundColor(.secondary)
                    Text(appState.searchText.isEmpty ? "No commands yet" : "No results")
                        .font(.title3)
                        .foregroundColor(.secondary)
                    Text(appState.searchText.isEmpty
                         ? "Click \"Add Command\" to get started."
                         : "Try a different search term.")
                        .font(.subheadline)
                        .foregroundColor(.secondary.opacity(0.7))
                }
                Spacer()
            } else {
                ScrollView {
                    LazyVStack(spacing: 8) {
                        ForEach(appState.filteredCommands) { cmd in
                            CommandRowView(command: cmd)
                                .environmentObject(appState)
                        }
                    }
                    .padding(.horizontal, 16)
                    .padding(.vertical, 12)
                }
            }
        }
        .sheet(isPresented: $appState.showAddEditSheet) {
            AddEditCommandSheet(command: appState.editingCommand)
                .environmentObject(appState)
        }
    }
}
