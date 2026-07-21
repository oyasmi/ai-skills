using System.Globalization;
using System.Windows;
using System.Windows.Data;
using System.Windows.Media;
using CmdMgr.ViewModels;

namespace CmdMgr.Views;

/// <summary>Converts a count to Visibility. With "invert" parameter: Visible when count is 0.</summary>
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

/// <summary>Collapses the element when the bound value is null or empty string.</summary>
public class NullToVisibilityConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
    {
        var visible = value switch
        {
            null => false,
            string s => !string.IsNullOrEmpty(s),
            _ => true
        };
        var invert = parameter is string p && p == "invert";
        if (invert) visible = !visible;
        return visible ? Visibility.Visible : Visibility.Collapsed;
    }

    public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        => throw new NotImplementedException();
}

/// <summary>Converts a bool to Visibility. With "invert" parameter: Visible when false.</summary>
public class BoolToVisibilityConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
    {
        var flag = value is bool b && b;
        var invert = parameter is string s && s == "invert";
        if (invert) flag = !flag;
        return flag ? Visibility.Visible : Visibility.Collapsed;
    }

    public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        => throw new NotImplementedException();
}

/// <summary>Two-way binds a RadioButton's IsChecked to whether an enum property equals ConverterParameter.</summary>
public class EnumToBooleanConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
        => value?.ToString() == parameter?.ToString();

    public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        => value is true && parameter != null ? Enum.Parse(targetType, (string)parameter) : Binding.DoNothing;
}

/// <summary>Visible only when the bound enum value's string form matches ConverterParameter.</summary>
public class EnumToVisibilityConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
        => value?.ToString() == parameter?.ToString() ? Visibility.Visible : Visibility.Collapsed;

    public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        => throw new NotImplementedException();
}

public class FilterDisplayNameConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
        => value is CommandFilter filter ? filter.DisplayName() : value?.ToString() ?? "";

    public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        => throw new NotImplementedException();
}

public class StatusDisplayNameConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
        => value is ProcessStatus status ? status.DisplayName() : "Not Run";

    public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        => throw new NotImplementedException();
}

public class StatusIconConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
        => value is ProcessStatus status ? status.Icon() : "○";

    public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        => throw new NotImplementedException();
}

/// <summary>Maps a nullable ProcessStatus to one of the theme brushes (MutedBrush when null/not run).</summary>
public class StatusToBrushConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
    {
        var key = value is ProcessStatus status ? status.BrushKey() : "MutedBrush";
        return System.Windows.Application.Current.TryFindResource(key) as Brush
               ?? Brushes.Gray;
    }

    public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        => throw new NotImplementedException();
}

/// <summary>Picks between two strings based on a bound bool. ConverterParameter format: "WhenTrue|WhenFalse".</summary>
public class BoolToStringConverter : IValueConverter
{
    public object Convert(object value, Type targetType, object parameter, CultureInfo culture)
    {
        var options = (parameter as string)?.Split('|') ?? new[] { "", "" };
        var flag = value is bool b && b;
        return flag ? options[0] : (options.Length > 1 ? options[1] : "");
    }

    public object ConvertBack(object value, Type targetType, object parameter, CultureInfo culture)
        => throw new NotImplementedException();
}

/// <summary>
/// Computes the primary action button's label, brush, or enabled state from
/// a command's (Status, CmdType) pair. ConverterParameter selects the aspect:
/// "label", "brush", or "enabled".
/// </summary>
public class RunButtonConverter : IMultiValueConverter
{
    public object Convert(object[] values, Type targetType, object parameter, CultureInfo culture)
    {
        var status = values.Length > 0 ? values[0] as ProcessStatus? : null;
        var cmdType = values.Length > 1 ? values[1] as string : null;
        var isLongRunning = cmdType == "long-running";
        var isRunning = status == ProcessStatus.Running;
        var hasRun = status.HasValue;

        return parameter as string switch
        {
            "label" => isRunning
                ? (isLongRunning ? "■ Stop" : "⏳ Running…")
                : (hasRun ? "▶ Run Again" : "▶ Run"),
            "brush" => System.Windows.Application.Current.TryFindResource(
                isRunning && isLongRunning ? "DangerBrush" : "PrimaryBrush") as Brush ?? Brushes.Gray,
            "enabled" => !(isRunning && !isLongRunning),
            _ => Binding.DoNothing
        };
    }

    public object[] ConvertBack(object value, Type[] targetTypes, object parameter, CultureInfo culture)
        => throw new NotImplementedException();
}
