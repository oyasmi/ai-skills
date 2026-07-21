using System.Windows.Media;
using CmdMgr.Models;

namespace CmdMgr.ViewModels;

/// <summary>Display-friendly wrapper around a raw <see cref="CommandHistory"/> row.</summary>
public class HistoryEntryViewModel
{
    public CommandHistory Entry { get; }

    public DateTime StartTime => Entry.StartTime;
    public string Duration => Entry.Duration ?? "";
    public bool HasOutput => !string.IsNullOrEmpty(Entry.Output);
    public string OutputDisplay => HasOutput ? Entry.Output! : "No output captured";

    public string StatusLabel => Entry.Status switch
    {
        "success" => "Succeeded",
        "failed" => "Failed",
        "terminated" => "Stopped",
        "running" => "Running",
        _ => Entry.Status
    };

    public Brush StatusBrush
    {
        get
        {
            var key = Entry.Status switch
            {
                "success" => "SuccessBrush",
                "failed" => "DangerBrush",
                "terminated" => "WarningBrush",
                "running" => "PrimaryBrush",
                _ => "MutedBrush"
            };
            return System.Windows.Application.Current.TryFindResource(key) as Brush ?? Brushes.Gray;
        }
    }

    public HistoryEntryViewModel(CommandHistory entry) => Entry = entry;
}
