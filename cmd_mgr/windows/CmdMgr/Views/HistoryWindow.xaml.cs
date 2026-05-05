using System.Globalization;
using System.Windows;
using System.Windows.Data;
using CmdMgr.Models;

namespace CmdMgr.Views;

public partial class HistoryWindow : Window
{
    public HistoryWindow(string commandName, List<CommandHistory> history)
    {
        Resources["NullToVisibilityConverter"] = new NullToVisibilityConverter();
        InitializeComponent();
        Title = $"History: {commandName}";

        if (history.Count == 0)
        {
            EmptyState.Visibility = Visibility.Visible;
            HistoryScroll.Visibility = Visibility.Collapsed;
        }
        else
        {
            HistoryList.ItemsSource = history;
        }
    }
}

/// <summary>Collapses the element when the bound value is null or empty string.</summary>
public class NullToVisibilityConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
        => value is string s && !string.IsNullOrEmpty(s)
            ? Visibility.Visible
            : Visibility.Collapsed;

    public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        => throw new NotImplementedException();
}
