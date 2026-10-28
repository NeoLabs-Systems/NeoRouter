import 'dart:math' as math;

import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';

double _num(Object? value) => value is num ? value.toDouble() : 0;
double? _maybe(Object? value) => value is num ? value.toDouble() : null;

class AnalyticsScreen extends StatefulWidget {
  const AnalyticsScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<AnalyticsScreen> createState() => _AnalyticsScreenState();
}

class _AnalyticsScreenState extends State<AnalyticsScreen> {
  String _range = '24h';
  Map<String, dynamic>? _data;
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final data = await widget.controller.api.analytics(_range);
      setState(() => _data = data);
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return PageScaffold(
      header: ScreenHeader(
        title: t.analyticsTitle,
        description: t.analyticsDescription,
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            SegmentedButton<String>(
              segments: <ButtonSegment<String>>[
                ButtonSegment<String>(value: '24h', label: Text(t.range24h)),
                ButtonSegment<String>(value: '7d', label: Text(t.range7d)),
                ButtonSegment<String>(value: '30d', label: Text(t.range30d)),
              ],
              selected: <String>{_range},
              onSelectionChanged: (value) {
                setState(() => _range = value.first);
                _load();
              },
            ),
            const SizedBox(width: AppSpacing.xs),
            IconButton(tooltip: t.refresh, onPressed: _load, icon: const Icon(Icons.refresh)),
          ],
        ),
      ),
      children: <Widget>[
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: _data == null ? const SizedBox.shrink() : _content(context, _data!),
        ),
      ],
    );
  }

  Widget _content(BuildContext context, Map<String, dynamic> data) {
    final t = AppL10n.of(context);
    final totals = Map<String, dynamic>.from(data['totals'] as Map);
    if (_num(totals['requests']) == 0) {
      return SectionCard(child: EmptyState(icon: Icons.insights_outlined, title: t.noTrafficTitle, message: t.noTrafficMessage));
    }
    final series = (data['series'] as List).cast<Map>();
    final bucketMs = _num(data['bucketMs']).toInt();
    final requests = _num(totals['requests']);
    final tiles = <Widget>[
      StatTile(icon: Icons.bolt_outlined, label: t.statRequests, value: formatCount(requests)),
      StatTile(icon: Icons.verified_outlined, label: t.statSuccessRate, value: '${(_num(totals['successRate']) * 100).toStringAsFixed(1)} %'),
      StatTile(icon: Icons.speed_outlined, label: '${t.chartLatency} p50', value: formatMs(_maybe(totals['latencyP50'])), caption: 'p95 ${formatMs(_maybe(totals['latencyP95']))}'),
      StatTile(icon: Icons.savings_outlined, label: t.statFreeShare, value: '${(_num(totals['freeRequests']) / requests * 100).toStringAsFixed(0)} %'),
      StatTile(icon: Icons.payments_outlined, label: t.statCost, value: formatUsd(_num(totals['costUsd']))),
      StatTile(
        icon: Icons.token_outlined,
        label: t.statTokens,
        value: formatCount(_num(totals['inputTokens']) + _num(totals['outputTokens'])),
        caption: t.statInOut(formatCount(_num(totals['inputTokens'])), formatCount(_num(totals['outputTokens']))),
      ),
      StatTile(icon: Icons.alt_route_outlined, label: t.statFallbacks, value: formatCount(_num(totals['fallbacks']))),
      StatTile(icon: Icons.timer_outlined, label: 'TTFT', value: formatMs(_maybe(totals['ttftP50'])), caption: 'p50'),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (data['truncated'] == true) ...<Widget>[InlineError(message: t.analyticsTruncated), const SizedBox(height: AppSpacing.md)],
        _Grid(minWidth: 220, children: tiles),
        const SizedBox(height: AppSpacing.md),
        SectionCard(
          title: t.chartRequests,
          trailing: _Legend(items: <(String, Color)>[(t.chartSuccess, neoRouterPaletteOf(context).accent), (t.chartErrors, neoRouterPaletteOf(context).error)]),
          child: _RequestsChart(series: series, bucketMs: bucketMs),
        ),
        const SizedBox(height: AppSpacing.md),
        _Grid(
          minWidth: 420,
          children: <Widget>[
            SectionCard(
              title: t.chartLatency,
              trailing: _Legend(items: <(String, Color)>[(t.chartP50, neoRouterPaletteOf(context).secondary), (t.chartP95, neoRouterPaletteOf(context).warning)]),
              child: _LineChart(
                series: series,
                bucketMs: bucketMs,
                lines: <(String, Color)>[('latencyP50', neoRouterPaletteOf(context).secondary), ('latencyP95', neoRouterPaletteOf(context).warning)],
                format: (value) => formatMs(value),
              ),
            ),
            SectionCard(
              title: t.chartTokens,
              trailing: _Legend(items: <(String, Color)>[(t.chartInput, neoRouterPaletteOf(context).info), (t.chartOutput, neoRouterPaletteOf(context).accent)]),
              child: _LineChart(
                series: series,
                bucketMs: bucketMs,
                lines: <(String, Color)>[('inputTokens', neoRouterPaletteOf(context).info), ('outputTokens', neoRouterPaletteOf(context).accent)],
                format: (value) => formatCount(value),
                filled: true,
              ),
            ),
            SectionCard(
              title: t.chartFreeShare,
              child: _LineChart(
                series: series.map((item) => <String, Object?>{
                      ...Map<String, Object?>.from(item),
                      'freeShare': _num(item['requests']) == 0 ? null : _num(item['freeRequests']) / _num(item['requests']) * 100,
                    }).toList(),
                bucketMs: bucketMs,
                lines: <(String, Color)>[('freeShare', neoRouterPaletteOf(context).success)],
                format: (value) => '${value.round()} %',
                maxY: 100,
                filled: true,
              ),
            ),
            SectionCard(
              title: t.chartCost,
              child: _LineChart(
                series: series,
                bucketMs: bucketMs,
                lines: <(String, Color)>[('costUsd', neoRouterPaletteOf(context).accentStrong)],
                format: (value) => formatUsd(value),
                filled: true,
              ),
            ),
          ],
        ),
        const SizedBox(height: AppSpacing.md),
        _Grid(
          minWidth: 420,
          children: <Widget>[
            SectionCard(title: t.providerShare, child: _ProviderDonut(items: (data['byProvider'] as List).cast<Map>())),
            SectionCard(
              title: t.attemptOutcomes,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: <Widget>[
                  Text(t.attemptOutcomesHint, style: TextStyle(color: neoRouterPaletteOf(context).textMuted, fontSize: 12)),
                  const SizedBox(height: AppSpacing.sm),
                  _HorizontalBars(items: (data['attemptOutcomes'] as List).cast<Map>().map((item) => ('${item['key']}', _num(item['count']))).toList(), colorOf: _outcomeColor),
                ],
              ),
            ),
            SectionCard(title: t.latencyDistribution, child: _Histogram(buckets: (data['latencyHistogram'] as List).cast<Map>())),
            SectionCard(
              title: t.apiFormats,
              child: _HorizontalBars(items: (data['byFormat'] as List).cast<Map>().map((item) => ('${item['key']}', _num(item['requests']))).toList()),
            ),
          ],
        ),
        const SizedBox(height: AppSpacing.md),
        SectionCard(title: t.modelTable, child: _GroupTable(label: t.modelTable, rows: (data['byModel'] as List).cast<Map>())),
        const SizedBox(height: AppSpacing.md),
        SectionCard(title: t.keyUsage, child: _GroupTable(label: t.keyUsage, rows: (data['byKey'] as List).cast<Map>())),
      ],
    );
  }

  Color _outcomeColor(BuildContext context, String key) {
    final palette = neoRouterPaletteOf(context);
    if (key == 'success') return palette.success;
    if (key == 'hedge_lost') return palette.info;
    if (key == 'rate_limit' || key == 'quota_exhausted') return palette.warning;
    return palette.error;
  }
}

/// Responsive grid: as many columns of at least [minWidth] as fit.
class _Grid extends StatelessWidget {
  const _Grid({required this.minWidth, required this.children});

  final double minWidth;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final columns = math.max(1, math.min(children.length, (constraints.maxWidth / minWidth).floor()));
        final width = (constraints.maxWidth - (columns - 1) * AppSpacing.md) / columns;
        return Wrap(
          spacing: AppSpacing.md,
          runSpacing: AppSpacing.md,
          children: <Widget>[for (final child in children) SizedBox(width: width, child: child)],
        );
      },
    );
  }
}

class _Legend extends StatelessWidget {
  const _Legend({required this.items});

  final List<(String, Color)> items;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Wrap(
      spacing: AppSpacing.sm,
      children: <Widget>[
        for (final (label, color) in items)
          Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Container(width: 10, height: 10, decoration: BoxDecoration(color: color, borderRadius: BorderRadius.circular(3))),
              const SizedBox(width: 5),
              Text(label, style: TextStyle(color: palette.textSoft, fontSize: 12)),
            ],
          ),
      ],
    );
  }
}

String _bucketLabel(int bucket, int bucketMs) {
  final time = DateTime.fromMillisecondsSinceEpoch(bucket);
  String two(int value) => value.toString().padLeft(2, '0');
  return bucketMs < 86_400_000 && bucketMs <= 3_600_000 ? '${two(time.hour)}:00' : '${two(time.day)}.${two(time.month)}.';
}

FlTitlesData _titles(BuildContext context, List<Map> series, int bucketMs, String Function(double) format) {
  final palette = neoRouterPaletteOf(context);
  final style = TextStyle(color: palette.textMuted, fontSize: 12);
  final step = math.max(1, (series.length / 6).ceil());
  return FlTitlesData(
    topTitles: const AxisTitles(),
    rightTitles: const AxisTitles(),
    leftTitles: AxisTitles(
      sideTitles: SideTitles(
        showTitles: true,
        reservedSize: 52,
        getTitlesWidget: (value, meta) => value == meta.max || value == meta.min && value == 0
            ? const SizedBox.shrink()
            : Padding(padding: const EdgeInsets.only(right: 6), child: Text(format(value), style: style, textAlign: TextAlign.right)),
      ),
    ),
    bottomTitles: AxisTitles(
      sideTitles: SideTitles(
        showTitles: true,
        reservedSize: 24,
        interval: 1,
        getTitlesWidget: (value, meta) {
          final index = value.toInt();
          if (index < 0 || index >= series.length || index % step != 0) return const SizedBox.shrink();
          return Padding(padding: const EdgeInsets.only(top: 6), child: Text(_bucketLabel(_num(series[index]['bucket']).toInt(), bucketMs), style: style));
        },
      ),
    ),
  );
}

FlGridData _grid(BuildContext context) {
  final palette = neoRouterPaletteOf(context);
  return FlGridData(drawVerticalLine: false, getDrawingHorizontalLine: (_) => FlLine(color: palette.border, strokeWidth: 1));
}

class _RequestsChart extends StatelessWidget {
  const _RequestsChart({required this.series, required this.bucketMs});

  final List<Map> series;
  final int bucketMs;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final width = math.max(2.0, math.min(18.0, 600 / math.max(1, series.length)));
    return SizedBox(
      height: 220,
      child: BarChart(
        BarChartData(
          gridData: _grid(context),
          borderData: FlBorderData(show: false),
          titlesData: _titles(context, series, bucketMs, (value) => formatCount(value)),
          barTouchData: BarTouchData(
            touchTooltipData: BarTouchTooltipData(
              getTooltipColor: (_) => palette.surfaceRaised,
              getTooltipItem: (group, _, rod, _) {
                final item = series[group.x];
                return BarTooltipItem(
                  '${_bucketLabel(_num(item['bucket']).toInt(), bucketMs)}\n${_num(item['successes']).toInt()} ok · ${_num(item['errors']).toInt()} err',
                  TextStyle(color: palette.text, fontSize: 12),
                );
              },
            ),
          ),
          barGroups: <BarChartGroupData>[
            for (var index = 0; index < series.length; index++)
              BarChartGroupData(
                x: index,
                barRods: <BarChartRodData>[
                  BarChartRodData(
                    toY: _num(series[index]['requests']),
                    width: width,
                    borderRadius: const BorderRadius.vertical(top: Radius.circular(3)),
                    rodStackItems: <BarChartRodStackItem>[
                      BarChartRodStackItem(0, _num(series[index]['successes']), palette.accent),
                      BarChartRodStackItem(_num(series[index]['successes']), _num(series[index]['requests']), palette.error),
                    ],
                    color: palette.accent,
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }
}

class _LineChart extends StatelessWidget {
  const _LineChart({required this.series, required this.bucketMs, required this.lines, required this.format, this.maxY, this.filled = false});

  final List<Map> series;
  final int bucketMs;
  final List<(String, Color)> lines;
  final String Function(double) format;
  final double? maxY;
  final bool filled;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return SizedBox(
      height: 180,
      child: LineChart(
        LineChartData(
          minY: 0,
          maxY: maxY,
          gridData: _grid(context),
          borderData: FlBorderData(show: false),
          titlesData: _titles(context, series, bucketMs, format),
          lineTouchData: LineTouchData(
            touchTooltipData: LineTouchTooltipData(
              getTooltipColor: (_) => palette.surfaceRaised,
              getTooltipItems: (spots) => spots
                  .map((spot) => LineTooltipItem(format(spot.y), TextStyle(color: spot.bar.color ?? palette.text, fontSize: 12, fontWeight: FontWeight.w600)))
                  .toList(),
            ),
          ),
          lineBarsData: <LineChartBarData>[
            for (final (field, color) in lines)
              LineChartBarData(
                spots: <FlSpot>[
                  for (var index = 0; index < series.length; index++)
                    _maybe(series[index][field]) == null ? FlSpot.nullSpot : FlSpot(index.toDouble(), _num(series[index][field])),
                ],
                isCurved: true,
                preventCurveOverShooting: true,
                color: color,
                barWidth: 2,
                dotData: const FlDotData(show: false),
                belowBarData: BarAreaData(show: filled, color: color.withValues(alpha: 0.12)),
              ),
          ],
        ),
      ),
    );
  }
}

class _ProviderDonut extends StatelessWidget {
  const _ProviderDonut({required this.items});

  final List<Map> items;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final colors = <Color>[palette.accent, palette.secondary, palette.info, palette.success, palette.warning, palette.error, palette.accentStrong, palette.textSoft];
    final total = items.fold<double>(0, (sum, item) => sum + _num(item['requests']));
    return Row(
      children: <Widget>[
        SizedBox(
          width: 160,
          height: 160,
          child: PieChart(
            PieChartData(
              centerSpaceRadius: 46,
              sectionsSpace: 2,
              sections: <PieChartSectionData>[
                for (var index = 0; index < items.length; index++)
                  PieChartSectionData(value: _num(items[index]['requests']), color: colors[index % colors.length], radius: 26, showTitle: false),
              ],
            ),
          ),
        ),
        const SizedBox(width: AppSpacing.md),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: <Widget>[
              for (var index = 0; index < items.length; index++)
                Padding(
                  padding: const EdgeInsets.only(bottom: 6),
                  child: Row(
                    children: <Widget>[
                      Container(width: 10, height: 10, decoration: BoxDecoration(color: colors[index % colors.length], borderRadius: BorderRadius.circular(3))),
                      const SizedBox(width: 8),
                      Expanded(child: Text('${items[index]['key']}', overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 13, fontWeight: FontWeight.w600))),
                      Text('${(_num(items[index]['requests']) / total * 100).toStringAsFixed(0)} %', style: TextStyle(color: palette.textMuted, fontSize: 12)),
                    ],
                  ),
                ),
            ],
          ),
        ),
      ],
    );
  }
}

class _HorizontalBars extends StatelessWidget {
  const _HorizontalBars({required this.items, this.colorOf});

  final List<(String, double)> items;
  final Color Function(BuildContext, String)? colorOf;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final max = items.fold<double>(0, (value, item) => math.max(value, item.$2));
    return Column(
      children: <Widget>[
        for (final (label, value) in items)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: Row(
              children: <Widget>[
                SizedBox(width: 130, child: Text(label, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 12.5, fontWeight: FontWeight.w600))),
                Expanded(
                  child: ClipRRect(
                    borderRadius: BorderRadius.circular(AppRadius.pill),
                    child: LinearProgressIndicator(
                      minHeight: 8,
                      value: max == 0 ? 0 : value / max,
                      backgroundColor: palette.surfaceMuted,
                      valueColor: AlwaysStoppedAnimation<Color>(colorOf?.call(context, label) ?? palette.accent),
                    ),
                  ),
                ),
                SizedBox(width: 56, child: Text(formatCount(value), textAlign: TextAlign.end, style: TextStyle(color: palette.textMuted, fontSize: 12))),
              ],
            ),
          ),
      ],
    );
  }
}

class _Histogram extends StatelessWidget {
  const _Histogram({required this.buckets});

  final List<Map> buckets;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    String label(int index) {
      final upTo = _maybe(buckets[index]['upToMs']);
      String short(double ms) => ms >= 1000 ? '${(ms / 1000).toStringAsFixed(0)}s' : '${ms.round()}';
      if (upTo != null) return t.upTo(short(upTo));
      return t.over(short(_maybe(buckets[index - 1]['upToMs']) ?? 0));
    }

    return SizedBox(
      height: 180,
      child: BarChart(
        BarChartData(
          gridData: _grid(context),
          borderData: FlBorderData(show: false),
          titlesData: FlTitlesData(
            topTitles: const AxisTitles(),
            rightTitles: const AxisTitles(),
            leftTitles: const AxisTitles(),
            bottomTitles: AxisTitles(
              sideTitles: SideTitles(
                showTitles: true,
                reservedSize: 24,
                getTitlesWidget: (value, meta) => Padding(
                  padding: const EdgeInsets.only(top: 6),
                  child: Text(label(value.toInt()), style: TextStyle(color: palette.textMuted, fontSize: 12)),
                ),
              ),
            ),
          ),
          barTouchData: BarTouchData(
            touchTooltipData: BarTouchTooltipData(
              getTooltipColor: (_) => palette.surfaceRaised,
              getTooltipItem: (group, _, rod, _) => BarTooltipItem('${rod.toY.toInt()}', TextStyle(color: palette.text, fontSize: 12)),
            ),
          ),
          barGroups: <BarChartGroupData>[
            for (var index = 0; index < buckets.length; index++)
              BarChartGroupData(
                x: index,
                barRods: <BarChartRodData>[
                  BarChartRodData(
                    toY: _num(buckets[index]['count']),
                    width: 22,
                    color: palette.secondary,
                    borderRadius: const BorderRadius.vertical(top: Radius.circular(4)),
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }
}

class _GroupTable extends StatelessWidget {
  const _GroupTable({required this.label, required this.rows});

  final String label;
  final List<Map> rows;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final head = TextStyle(color: palette.textMuted, fontSize: 12, fontWeight: FontWeight.w700);
    const cell = TextStyle(fontSize: 13);
    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      child: DataTable(
        headingRowHeight: 36,
        dataRowMinHeight: 36,
        dataRowMaxHeight: 40,
        horizontalMargin: 0,
        columnSpacing: AppSpacing.lg,
        dividerThickness: 0.5,
        columns: <DataColumn>[
          DataColumn(label: Text(label.toUpperCase(), style: head)),
          DataColumn(numeric: true, label: Text(t.colRequests.toUpperCase(), style: head)),
          DataColumn(numeric: true, label: Text(t.colSuccess.toUpperCase(), style: head)),
          DataColumn(numeric: true, label: Text(t.colLatency.toUpperCase(), style: head)),
          DataColumn(numeric: true, label: Text(t.colTokens.toUpperCase(), style: head)),
          DataColumn(numeric: true, label: Text(t.colCost.toUpperCase(), style: head)),
        ],
        rows: <DataRow>[
          for (final row in rows)
            DataRow(
              cells: <DataCell>[
                DataCell(Text('${row['key']}', style: const TextStyle(fontFamily: 'monospace', fontSize: 12.5))),
                DataCell(Text(formatCount(_num(row['requests'])), style: cell)),
                DataCell(Text(
                  '${(_num(row['successRate']) * 100).toStringAsFixed(0)} %',
                  style: cell.copyWith(color: _num(row['successRate']) >= 0.95 ? palette.success : _num(row['successRate']) >= 0.8 ? palette.warning : palette.error),
                )),
                DataCell(Text('${formatMs(_maybe(row['latencyP50']))} / ${formatMs(_maybe(row['latencyP95']))}', style: cell)),
                DataCell(Text(formatCount(_num(row['inputTokens']) + _num(row['outputTokens'])), style: cell)),
                DataCell(Text(formatUsd(_num(row['costUsd'])), style: cell)),
              ],
            ),
        ],
      ),
    );
  }
}
