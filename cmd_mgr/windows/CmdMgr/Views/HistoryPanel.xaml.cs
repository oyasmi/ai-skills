using System.Windows;
using System.Windows.Controls;
using CmdMgr.Models;
using CmdMgr.ViewModels;

namespace CmdMgr.Views;

public partial class HistoryPanel : UserControl
{
    private Action? _runAgain;

    public HistoryPanel()
    {
        InitializeComponent();
    }

    public void Attach(List<CommandHistory> history, Action runAgain)
    {
        _runAgain = runAgain;

        if (history.Count == 0)
        {
            EmptyState.Visibility = Visibility.Visible;
            HistoryScroll.Visibility = Visibility.Collapsed;
            CountText.Text = "No runs";
        }
        else
        {
            EmptyState.Visibility = Visibility.Collapsed;
            HistoryScroll.Visibility = Visibility.Visible;
            CountText.Text = $"{history.Count} run{(history.Count == 1 ? "" : "s")}";
            HistoryList.ItemsSource = history.Select(h => new HistoryEntryViewModel(h)).ToList();
        }
    }

    private void RunAgain_Click(object sender, RoutedEventArgs e) => _runAgain?.Invoke();

    private void CopyEntry_Click(object sender, RoutedEventArgs e)
    {
        if (sender is Button { Tag: HistoryEntryViewModel entry } && entry.HasOutput)
            System.Windows.Clipboard.SetText(entry.Entry.Output);
    }
}
