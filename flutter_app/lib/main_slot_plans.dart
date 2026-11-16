import 'dart:math' as math;

import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_shared.dart';
import 'main_theme.dart';

class SlotOverflowControls extends StatelessWidget {
  const SlotOverflowControls({
    super.key,
    required this.policy,
    required this.onChanged,
    required this.wait,
  });
  final String policy;
  final ValueChanged<String> onChanged;
  final TextEditingController wait;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        DropdownButtonFormField<String>(
          initialValue: policy,
          isExpanded: true,
          decoration: InputDecoration(labelText: t.slotOverflowPolicy),
          items: [
            DropdownMenuItem(
              value: 'auto',
              child: Text(
                t.slotOverflowAuto,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
              ),
            ),
            DropdownMenuItem(value: 'overflow', child: Text(t.slotOverflowNow)),
            DropdownMenuItem(value: 'queue', child: Text(t.slotOverflowQueue)),
          ],
          onChanged: (value) {
            if (value != null) onChanged(value);
          },
        ),
        if (policy != 'overflow')
          TextField(
            controller: wait,
            keyboardType: TextInputType.number,
            decoration: InputDecoration(labelText: t.slotMaxWait),
          ),
      ],
    );
  }
}

class SlotUsage extends StatelessWidget {
  const SlotUsage({super.key, required this.usage});
  final Map usage;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final series = (usage['series'] as List? ?? []).cast<Map>();
    String value(Object? number) =>
        number is num ? number.toStringAsFixed(2) : '—';
    final peak = (usage['peakBusy'] as num? ?? 0).toDouble();
    final limit = (usage['limit'] as num?)?.toDouble();
    final origin = series.isEmpty
        ? 0
        : (series.first['bucket'] as num).toDouble();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SettingsRow(
          label: t.slotCurrent,
          control: Text('${usage['current']} / ${usage['limit'] ?? '—'}'),
        ),
        SettingsRow(
          label: t.slotAverage,
          control: Text(value(usage['averageBusy'])),
        ),
        SettingsRow(label: t.slotPeak, control: Text('$peak')),
        SettingsRow(
          label: t.slotIdle,
          control: Text(
            usage['idleShare'] is num
                ? '${((usage['idleShare'] as num) * 100).toStringAsFixed(1)} %'
                : '—',
          ),
        ),
        if (series.any((row) => row['averageBusy'] != null))
          Padding(
            padding: const EdgeInsets.all(18),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(
                  t.slotObserved,
                  style: TextStyle(color: palette.textSoft, fontSize: 12),
                ),
                const SizedBox(height: 12),
                SizedBox(
                  height: 110,
                  child: LineChart(
                    LineChartData(
                      minY: 0,
                      maxY: math.max(1, math.max(limit ?? 0, peak)),
                      gridData: const FlGridData(show: false),
                      borderData: FlBorderData(show: false),
                      titlesData: const FlTitlesData(show: false),
                      lineTouchData: const LineTouchData(enabled: false),
                      lineBarsData: [
                        LineChartBarData(
                          color: palette.accent,
                          barWidth: 2,
                          isStepLineChart: true,
                          dotData: const FlDotData(show: false),
                          spots: [
                            for (final row in series)
                              if (row['averageBusy'] is num)
                                FlSpot(
                                  ((row['bucket'] as num).toDouble() - origin) /
                                      60000,
                                  (row['averageBusy'] as num).toDouble(),
                                ),
                          ],
                          belowBarData: BarAreaData(
                            show: true,
                            color: palette.accent.withValues(alpha: 0.12),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ],
            ),
          ),
      ],
    );
  }
}
