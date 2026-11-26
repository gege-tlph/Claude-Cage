namespace Cage.Windows;

// The window of an update in progress: the versions, then one row per step
// with its state, and the result when it fails. Names and states only.
internal sealed class UpdateWindow : Form
{
    private readonly List<(Label Mark, Label Name, ProgressBar Bar)> steps = new();
    private readonly Label status = new() { AutoSize = true, Visible = false, Margin = new Padding(0, 12, 0, 0) };
    private readonly FlowLayoutPanel actions = new() { AutoSize = true, FlowDirection = FlowDirection.RightToLeft, Dock = DockStyle.Fill, Visible = false, Margin = new Padding(0, 12, 0, 0) };

    public UpdateWindow(string? versions, IReadOnlyList<string> stepNames)
    {
        Text = InterfaceLanguage.Text("Cage lite 更新");
        Icon = Icon.ExtractAssociatedIcon(Environment.ProcessPath!) ?? SystemIcons.Application;
        AutoScaleMode = AutoScaleMode.Dpi; AutoScaleDimensions = new SizeF(96, 96);
        Font = SystemFonts.MessageBoxFont ?? Font;
        FormBorderStyle = FormBorderStyle.FixedSingle; MaximizeBox = false;
        StartPosition = FormStartPosition.CenterScreen;
        AutoSize = true; AutoSizeMode = AutoSizeMode.GrowAndShrink;
        MinimumSize = new Size(360, 0);
        var layout = new TableLayoutPanel { AutoSize = true, ColumnCount = 3, Padding = new Padding(24, 20, 24, 20), Dock = DockStyle.Fill };
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 24));
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.AutoSize));
        layout.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 100));
        if (versions != null)
        {
            var header = new Label { Text = versions, AutoSize = true, Font = new Font(Font.FontFamily, Font.Size * 1.3f, FontStyle.Bold), Margin = new Padding(0, 0, 0, 14) };
            layout.Controls.Add(header, 0, 0); layout.SetColumnSpan(header, 3);
        }
        foreach (var name in stepNames)
        {
            var row = (Mark: new Label { AutoSize = true, Margin = new Padding(0, 4, 4, 4) },
                Name: new Label { Text = InterfaceLanguage.Text(name), AutoSize = true, Margin = new Padding(0, 4, 16, 4) },
                Bar: new ProgressBar { Width = 160, Height = 10, Visible = false, Anchor = AnchorStyles.Left, Margin = new Padding(0, 7, 0, 4) });
            var line = layout.RowCount++;
            layout.Controls.Add(row.Mark, 0, line); layout.Controls.Add(row.Name, 1, line); layout.Controls.Add(row.Bar, 2, line);
            steps.Add(row);
        }
        var statusLine = layout.RowCount++;
        layout.Controls.Add(status, 0, statusLine); layout.SetColumnSpan(status, 3);
        var actionLine = layout.RowCount++;
        layout.Controls.Add(actions, 0, actionLine); layout.SetColumnSpan(actions, 3);
        Controls.Add(layout);
        SetStep(-1, null);
    }

    // Steps before index are done; index runs, with its percent or, unknown,
    // a moving bar; the rest wait.
    public void SetStep(int index, int? percent)
    {
        for (var i = 0; i < steps.Count; i++)
        {
            var (mark, name, bar) = steps[i];
            mark.Text = i < index ? "✓" : i == index ? "●" : "○";
            mark.ForeColor = i <= index ? SystemColors.ControlText : SystemColors.GrayText;
            name.ForeColor = mark.ForeColor;
            bar.Visible = i == index;
            if (i != index) continue;
            bar.Style = percent == null ? ProgressBarStyle.Marquee : ProgressBarStyle.Continuous;
            if (percent is { } value) bar.Value = Math.Clamp(value, 0, 100);
        }
    }

    // The step at index failed: the result line and what can be done next.
    public void Fail(int index, string result, params (string Text, Action Click)[] next)
    {
        SetStep(index, 0);
        if (index >= 0 && index < steps.Count)
        {
            steps[index].Mark.Text = "✗";
            steps[index].Bar.Visible = false;
        }
        status.Text = result; status.Visible = true;
        actions.Controls.Clear();
        // Right to left: the first action is the rightmost, the default one.
        foreach (var (text, click) in next)
        {
            var button = new Button { Text = InterfaceLanguage.Text(text), AutoSize = true, Margin = new Padding(8, 0, 0, 0) };
            button.Click += (_, _) => click();
            actions.Controls.Add(button);
        }
        actions.Visible = next.Length > 0;
        if (actions.Controls.Count > 0) AcceptButton = (IButtonControl)actions.Controls[0];
    }
}
