import SwiftUI
import AppKit

final class AppDelegate: NSObject, NSApplicationDelegate {
    weak var appState: AppState?

    func applicationShouldTerminate(_ sender: NSApplication) -> NSApplication.TerminateReply {
        guard let appState = appState,
              !appState.processManager.runningProcesses.isEmpty else {
            return .terminateNow
        }

        let alert = NSAlert()
        alert.messageText = "Running Processes"
        alert.informativeText = "There are still running commands. Do you want to terminate them and exit?"
        alert.addButton(withTitle: "Terminate and Exit")
        alert.addButton(withTitle: "Cancel")
        alert.alertStyle = .warning

        if alert.runModal() == .alertFirstButtonReturn {
            // Snapshot running processes before stopAll modifies the dictionary.
            let running = Array(appState.processManager.runningProcesses.values)
            appState.processManager.stopAll(database: appState.database)

            // Wait on a background thread so the main run loop stays live
            // and can process the terminationHandler dispatches (DB writes).
            DispatchQueue.global().async {
                appState.processManager.waitForAll(processes: running)
                DispatchQueue.main.async {
                    NSApp.reply(toApplicationShouldTerminate: true)
                }
            }
            return .terminateLater
        }

        return .terminateCancel
    }
}

@main
struct CmdMgrApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate
    @StateObject private var appState = AppState()

    var body: some Scene {
        WindowGroup {
            ContentView()
                .environmentObject(appState)
                .frame(minWidth: 760, minHeight: 520)
                .onAppear {
                    appDelegate.appState = appState
                }
        }
        .windowStyle(.titleBar)
        .windowToolbarStyle(.unified)
        .defaultSize(width: 1040, height: 680)
        .commands {
            CommandGroup(after: .newItem) {
                Button("Run Selected Command") {
                    appState.runSelectedCommand()
                }
                .keyboardShortcut(.return, modifiers: .command)
                .disabled(appState.selectedCommand == nil)
            }
        }
    }
}
