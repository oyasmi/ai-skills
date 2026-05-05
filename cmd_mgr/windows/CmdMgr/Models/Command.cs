using System.ComponentModel;
using System.Runtime.CompilerServices;

namespace CmdMgr.Models;

/// <summary>
/// A saved command configuration – matches the Python schema.
/// </summary>
public class Command : INotifyPropertyChanged
{
    public event PropertyChangedEventHandler? PropertyChanged;

    public int Id { get; set; }
    public string Name { get; set; } = "";
    public string CommandText { get; set; } = "";
    public string CmdType { get; set; } = "one-shot"; // "long-running" | "one-shot"
    public string? WorkingDirectory { get; set; }
    public DateTime CreatedAt { get; set; } = DateTime.Now;

    private bool _isRunning;
    public bool IsRunning
    {
        get => _isRunning;
        set
        {
            if (_isRunning == value) return;
            _isRunning = value;
            OnPropertyChanged();
        }
    }

    // Set to true once the process has completed at least one run with output.
    // Enables the "View Output" button even after the process has stopped.
    private bool _hasLastOutput;
    public bool HasLastOutput
    {
        get => _hasLastOutput;
        set
        {
            if (_hasLastOutput == value) return;
            _hasLastOutput = value;
            OnPropertyChanged();
        }
    }

    public bool IsLongRunning => CmdType == "long-running";
    public bool IsOneShot => CmdType == "one-shot";

    public string DisplayCommand => CommandText.Length > 80
        ? CommandText[..80] + "…"
        : CommandText;

    private void OnPropertyChanged([CallerMemberName] string? name = null)
        => PropertyChanged?.Invoke(this, new PropertyChangedEventArgs(name));
}
