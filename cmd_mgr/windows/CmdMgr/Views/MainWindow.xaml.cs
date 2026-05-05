using System.Globalization;
using System.Windows;
using System.Windows.Data;
using CmdMgr.ViewModels;

namespace CmdMgr.Views;

public partial class MainWindow : Window
{
    public MainWindow()
    {
        Resources["CountToVisibilityConverter"] = new CountToVisibilityConverter();
        InitializeComponent();
    }

    private void Window_Closing(object sender, System.ComponentModel.CancelEventArgs e)
    {
        if (DataContext is MainViewModel vm)
        {
            e.Cancel = !vm.OnClosing();
            if (!e.Cancel)
                vm.Dispose();
        }
    }
}

/// <summary>
/// Converts a count to Visibility. With "invert" parameter: Visible when count is 0.
/// </summary>
public class CountToVisibilityConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
    {
        var count = value is int i ? i : 0;
        var invert = parameter is string s && s == "invert";
        if (invert)
            return count == 0 ? Visibility.Visible : Visibility.Collapsed;
        return count > 0 ? Visibility.Visible : Visibility.Collapsed;
    }

    public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        => throw new NotImplementedException();
}
