using System.Diagnostics;
using System.Collections.Concurrent;
using System.Text;

namespace CmdMgr.ViewModels;

/// <summary>
/// Information about a running (or recently completed) process.
/// </summary>
public class ProcessInfo : ViewModelBase
{
    private const int MaxLines = 5000;

    public int CommandId { get; }
    public Models.Command Command { get; }
    public Process Process { get; }
    public int HistoryId { get; }

    private string _output = "";
    public string Output
    {
        get => _output;
        set => SetField(ref _output, value);
    }

    private bool _isRunning = true;
    public bool IsRunning
    {
        get => _isRunning;
        set => SetField(ref _isRunning, value);
    }

    public ProcessInfo(Models.Command command, Process process, int historyId)
    {
        Command = command;
        CommandId = command.Id;
        Process = process;
        HistoryId = historyId;
    }

    /// <summary>Appends text and trims to MaxLines to prevent unbounded memory growth.</summary>
    public void AppendOutput(string text)
    {
        var combined = _output + text;
        var lines = combined.Split('\n');
        Output = lines.Length > MaxLines
            ? string.Join("\n", lines[^MaxLines..])
            : combined;
    }
}

/// <summary>
/// Manages subprocess lifecycle on Windows.
/// </summary>
public class ProcessManager
{
    public ConcurrentDictionary<int, ProcessInfo> RunningProcesses { get; } = new();
    // Keeps last completed run per command so output remains viewable after exit.
    public ConcurrentDictionary<int, ProcessInfo> CompletedProcesses { get; } = new();

    private readonly ConcurrentDictionary<int, bool> _requestedStops = new();

    public bool IsRunning(int commandId) =>
        RunningProcesses.TryGetValue(commandId, out var info) && info.IsRunning;

    public ProcessInfo? GetProcessInfo(int commandId) =>
        RunningProcesses.TryGetValue(commandId, out var r) ? r :
        CompletedProcesses.TryGetValue(commandId, out var c) ? c : null;

    public bool HasOutput(int commandId) =>
        !string.IsNullOrEmpty(GetProcessInfo(commandId)?.Output);

    public ProcessInfo? Start(Models.Command command, Models.Database database)
    {
        if (IsRunning(command.Id)) return null;

        int? historyId = null;
        try
        {
            Encoding encoding;
            try
            {
                encoding = Encoding.GetEncoding(
                    System.Globalization.CultureInfo.CurrentCulture.TextInfo.OEMCodePage);
            }
            catch
            {
                encoding = Encoding.UTF8;
            }

            var psi = new ProcessStartInfo
            {
                FileName = "cmd.exe",
                Arguments = $"/c {command.CommandText}",
                UseShellExecute = false,
                RedirectStandardOutput = true,
                RedirectStandardError = true,
                CreateNoWindow = true,
                StandardOutputEncoding = encoding,
                StandardErrorEncoding = encoding
            };

            if (!string.IsNullOrWhiteSpace(command.WorkingDirectory))
                psi.WorkingDirectory = command.WorkingDirectory;

            historyId = database.AddHistoryEntry(command.Id, DateTime.Now, "running");
            var proc = new Process { StartInfo = psi, EnableRaisingEvents = true };
            var info = new ProcessInfo(command, proc, historyId.Value);

            proc.OutputDataReceived += (_, e) =>
            {
                if (e.Data != null)
                    System.Windows.Application.Current?.Dispatcher.Invoke(
                        () => info.AppendOutput(e.Data + "\n"));
            };
            proc.ErrorDataReceived += (_, e) =>
            {
                if (e.Data != null)
                    System.Windows.Application.Current?.Dispatcher.Invoke(
                        () => info.AppendOutput($"[STDERR] {e.Data}\n"));
            };

            // Exited is the single source of truth for all cleanup and DB writes.
            proc.Exited += (_, _) =>
            {
                var wasRequestedStop = _requestedStops.TryRemove(command.Id, out _);
                var status = wasRequestedStop ? "terminated" : (proc.ExitCode == 0 ? "success" : "failed");
                System.Windows.Application.Current?.Dispatcher.Invoke(() =>
                {
                    info.IsRunning = false;
                    command.IsRunning = false;
                    if (!string.IsNullOrEmpty(info.Output))
                        command.HasLastOutput = true;
                    database.UpdateHistoryEntry(info.HistoryId, DateTime.Now, status, info.Output);
                    RunningProcesses.TryRemove(command.Id, out _);
                    CompletedProcesses[command.Id] = info;
                });
            };

            command.IsRunning = true;
            CompletedProcesses.TryRemove(command.Id, out _);
            RunningProcesses[command.Id] = info;

            if (!proc.Start())
            {
                command.IsRunning = false;
                RunningProcesses.TryRemove(command.Id, out _);
                database.UpdateHistoryEntry(historyId.Value, DateTime.Now, "failed",
                    "Launch error: process did not start.");
                return null;
            }

            proc.BeginOutputReadLine();
            proc.BeginErrorReadLine();
            return info;
        }
        catch (Exception ex)
        {
            command.IsRunning = false;
            RunningProcesses.TryRemove(command.Id, out _);
            if (historyId.HasValue)
                database.UpdateHistoryEntry(historyId.Value, DateTime.Now, "failed",
                    $"Launch error: {ex.Message}");
            System.Windows.MessageBox.Show(
                $"Failed to start command '{command.Name}':\n{ex.Message}",
                "Execution Error", System.Windows.MessageBoxButton.OK,
                System.Windows.MessageBoxImage.Error);
            return null;
        }
    }

    /// <summary>
    /// Sends a kill signal. All cleanup (IsRunning, DB update, dictionary removal)
    /// is handled exclusively by the Exited event handler to avoid race conditions.
    /// </summary>
    public void Stop(int commandId, Models.Database database)
    {
        if (!RunningProcesses.TryGetValue(commandId, out var info)) return;

        _requestedStops[commandId] = true;
        try
        {
            if (!info.Process.HasExited)
                info.Process.Kill(entireProcessTree: true);
        }
        catch { /* Process already exited – Exited handler will clean up */ }
    }

    public void StopAll(Models.Database database)
    {
        foreach (var cmdId in RunningProcesses.Keys)
            Stop(cmdId, database);
    }
}
