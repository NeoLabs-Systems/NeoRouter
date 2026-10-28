import 'dart:math' as math;

import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_dashboard.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/models.dart';

double _num(Object? value) => value is num ? value.toDouble() : 0;
double? _maybe(Object? value) => value is num ? value.toDouble() : null;

enum _ActivityTab { overview, requests }

/// What happened: an overview with a few charts, and the request list.
class ActivityScreen extends StatefulWidget {
  const ActivityScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<ActivityScreen> createState() => _ActivityScreenState();
}

class _ActivityScreenState extends State<ActivityScreen> {
  _ActivityTab _tab = _ActivityTab.overview;
  String _range = '7d';

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return PageScaffold(
      header: ScreenHeader(
        title: t.navActivity,
        description: t.activityDescription,
        trailing: _tab == _ActivityTab.overview
            ? SegmentedButton<String>(
                showSelectedIcon: false,
                segments: <ButtonSegment<String>>[
                  ButtonSegment<String>(value: '24h', label: Text(t.range24h)),
                  ButtonSegment<String>(value: '7d', label: Text(t.range7d)),
                  ButtonSegment<String>(value: '30d', label: Text(t.range30d)),
                ],
                selected: <String>{_range},
                onSelectionChanged: (value) => setState(() => _range = value.first),
              )
            : null,
      ),
      children: <Widget>[
        PageTabs<_ActivityTab>(
          tabs: <({_ActivityTab value, String label, int? count})>[
            (value: _ActivityTab.overview, label: t.tabOverview, count: null),
            (value: _ActivityTab.requests, label: t.tabRequests, count: null),
          ],
          selected: _tab,
          onChanged: (value) => setState(() => _tab = value),
        ),
        if (_tab == _ActivityTab.overview)
          _Overview(key: ValueKey<String>(_range), controller: widget.controller, range: _range)
        else
          _RequestList(controller: widget.controller),
      ],
    );
  }
}

class _Overview extends StatefulWidget {
  const _Overview({super.key, required this.controller, required this.range});

  final NeoRouterController controller;
  final String range;

  @override
  State<_Overview> createState() => _OverviewState();
}

class _OverviewState extends State<_Overview> {
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
      final data = await widget.controller.api.analytics(widget.range);
      setState(() => _data = data);
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return AsyncBody(loading: _loading, error: _error, onRetry: _load, child: _data == null ? const SizedBox.shrink() : _content(context, _data!));
  }

  Widget _content(BuildContext context, Map<String, dynamic> data) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final totals = Map<String, dynamic>.from(data['totals'] as Map);
    final requests = _num(totals['requests']);
    if (requests == 0) {
      return AppCard(
        child: EmptyState(
          icon: Icons.show_chart_rounded,
          title: t.noTrafficTitle,
          message: t.noTrafficMessage,
          action: FilledButton(onPressed: () => widget.controller.go(AppPage.playground), child: Text(t.homeOpenPlayground)),
        ),
      );
    }
    final series = (data['series'] as List).cast<Map>();
    final bucketMs = _num(data['bucketMs']).toInt();
    final tokens = _num(totals['inputTokens']) + _num(totals['outputTokens']);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        if (data['truncated'] == true) ...<Widget>[InlineHint(message: t.analyticsTruncated), const SizedBox(height: AppSpacing.md)],
        MetricStrip(
          metrics: <({String label, String value, String? caption})>[
            (label: t.metricRequests, value: formatCount(requests), caption: t.metricTokens(formatCount(tokens))),
            (label: t.metricAnswered, value: '${(_num(totals['successRate']) * 100).round()} %', caption: null),
            (label: t.metricTypicalResponse, value: formatMs(_maybe(totals['latencyP50'])), caption: t.metricSlowest(formatMs(_maybe(totals['latencyP95'])))),
            (
              label: t.metricSaved,
              value: formatUsd(_num(totals['savedUsd'])),
              caption: t.metricSavedCaption(formatUsd(_num(totals['costUsd'])), '${(_num(totals['freeRequests']) / requests * 100).round()}'),
            ),
          ],
        ),
        const SizedBox(height: 28),
        SectionCard(title: t.chartRequests, child: _RequestsChart(series: series, bucketMs: bucketMs)),
        const SizedBox(height: 28),
        SectionCard(
          title: t.chartResponseTime,
          trailing: _Legend(items: <(String, Color)>[(t.chartTypical, palette.text.withValues(alpha: 0.75)), (t.chartSlowest, palette.secondary)]),
          child: _LineChart(
            series: series,
            bucketMs: bucketMs,
            lines: <(String, Color)>[('latencyP50', palette.text.withValues(alpha: 0.75)), ('latencyP95', palette.secondary)],
            format: (value) => formatMs(value),
          ),
        ),
        const SizedBox(height: 28),
        LayoutBuilder(
          builder: (context, constraints) {
            final models = _Ranking(title: t.topModelsTitle, rows: (data['byModel'] as List).cast<Map>(), label: shortModelName);
            final providers = _Ranking(title: t.byConnection, rows: (data['byProvider'] as List).cast<Map>());
            final keys = _Ranking(title: t.byKey, rows: (data['byKey'] as List).cast<Map>());
            if (constraints.maxWidth < 860) {
              return Column(children: <Widget>[models, const SizedBox(height: 28), providers, const SizedBox(height: 28), keys]);
            }
            return Column(
              children: <Widget>[
                models,
                const SizedBox(height: 28),
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[Expanded(child: providers), const SizedBox(width: AppSpacing.lg), Expanded(child: keys)],
                ),
              ],
            );
          },
        ),
      ],
    );
  }
}

/// Top entries with a thin neutral bar for their share.
class _Ranking extends StatelessWidget {
  const _Ranking({required this.title, required this.rows, this.label});

  final String title;
  final List<Map> rows;
  final String Function(String)? label;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final top = rows.take(6).toList();
    final max = top.fold<double>(0, (value, row) => math.max(value, _num(row['requests'])));
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(title: title),
        SettingsCardList(
          children: <Widget>[
            for (final row in top)
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: <Widget>[
                    Row(
                      children: <Widget>[
                        Expanded(
                          child: Text(
                            label?.call('${row['key']}') ?? '${row['key']}',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(fontSize: 13.5, color: palette.text, fontWeight: FontWeight.w500),
                          ),
                        ),
                        Text(
                          t.rankingLine(formatCount(_num(row['requests'])), formatMs(_maybe(row['latencyP50']))),
                          style: TextStyle(fontSize: 12.5, color: palette.textMuted),
                        ),
                      ],
                    ),
                    const SizedBox(height: 8),
                    ClipRRect(
                      borderRadius: BorderRadius.circular(AppRadius.pill),
                      child: LinearProgressIndicator(
                        minHeight: 4,
                        value: max == 0 ? 0 : _num(row['requests']) / max,
                        backgroundColor: palette.surfaceRaised,
                        valueColor: AlwaysStoppedAnimation<Color>(palette.textSoft.withValues(alpha: 0.6)),
                      ),
                    ),
                  ],
                ),
              ),
          ],
        ),
      ],
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
      spacing: AppSpacing.md,
      children: <Widget>[
        for (final (label, color) in items)
          Row(
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              Container(width: 10, height: 3, decoration: BoxDecoration(color: color, borderRadius: BorderRadius.circular(2))),
              const SizedBox(width: 6),
              Text(label, style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
            ],
          ),
      ],
    );
  }
}

String _bucketLabel(int bucket, int bucketMs) {
  final time = DateTime.fromMillisecondsSinceEpoch(bucket);
  String two(int value) => value.toString().padLeft(2, '0');
  return bucketMs <= 3_600_000 ? '${two(time.hour)}:00' : '${two(time.day)}.${two(time.month)}.';
}

FlTitlesData _titles(BuildContext context, List<Map> series, int bucketMs, String Function(double) format) {
  final palette = neoRouterPaletteOf(context);
  final style = TextStyle(color: palette.textMuted, fontSize: 11.5);
  final step = math.max(1, (series.length / 6).ceil());
  return FlTitlesData(
    topTitles: const AxisTitles(),
    rightTitles: const AxisTitles(),
    leftTitles: AxisTitles(
      sideTitles: SideTitles(
        showTitles: true,
        reservedSize: 48,
        getTitlesWidget: (value, meta) => value == meta.max || value == 0
            ? const SizedBox.shrink()
            : Padding(padding: const EdgeInsets.only(right: 8), child: Text(format(value), style: style, textAlign: TextAlign.right)),
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
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final width = math.max(3.0, math.min(16.0, 560 / math.max(1, series.length)));
    return SizedBox(
      height: 200,
      child: BarChart(
        BarChartData(
          gridData: _grid(context),
          borderData: FlBorderData(show: false),
          titlesData: _titles(context, series, bucketMs, (value) => formatCount(value)),
          barTouchData: BarTouchData(
            touchTooltipData: BarTouchTooltipData(
              getTooltipColor: (_) => palette.text,
              getTooltipItem: (group, _, rod, _) {
                final item = series[group.x];
                return BarTooltipItem(
                  '${_bucketLabel(_num(item['bucket']).toInt(), bucketMs)}\n${t.tooltipRequests('${_num(item['requests']).toInt()}', '${_num(item['errors']).toInt()}')}',
                  TextStyle(color: palette.scaffold, fontSize: 12),
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
                      BarChartRodStackItem(_num(series[index]['successes']), _num(series[index]['requests']), palette.error.withValues(alpha: 0.7)),
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
  const _LineChart({required this.series, required this.bucketMs, required this.lines, required this.format});

  final List<Map> series;
  final int bucketMs;
  final List<(String, Color)> lines;
  final String Function(double) format;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return SizedBox(
      height: 180,
      child: LineChart(
        LineChartData(
          minY: 0,
          minX: 0,
          maxX: (series.length - 1).toDouble().clamp(0, double.infinity),
          gridData: _grid(context),
          borderData: FlBorderData(show: false),
          titlesData: _titles(context, series, bucketMs, format),
          lineTouchData: LineTouchData(
            touchTooltipData: LineTouchTooltipData(
              getTooltipColor: (_) => palette.text,
              getTooltipItems: (spots) => spots.map((spot) => LineTooltipItem(format(spot.y), TextStyle(color: palette.scaffold, fontSize: 12))).toList(),
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
                dotData: FlDotData(show: series.where((item) => _maybe(item[field]) != null).length < 3),
              ),
          ],
        ),
      ),
    );
  }
}

class _RequestList extends StatefulWidget {
  const _RequestList({required this.controller});

  final NeoRouterController controller;

  @override
  State<_RequestList> createState() => _RequestListState();
}

class _RequestListState extends State<_RequestList> {
  List<RequestLog> _logs = <RequestLog>[];
  bool _loading = true;
  String? _error;
  String? _status;
  String? _open;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final logs = await widget.controller.api.logs(limit: 200, status: _status);
      setState(() => _logs = logs);
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Row(
          children: <Widget>[
            Expanded(
              child: Wrap(
                spacing: 6,
                runSpacing: 6,
                children: <Widget>[
                  for (final (value, label) in <(String?, String)>[(null, t.filterAll), ('success', t.filterAnswered), ('error', t.filterFailed)])
                    ChoiceChip(
                      label: Text(label),
                      selected: _status == value,
                      onSelected: (_) {
                        setState(() => _status = value);
                        _load();
                      },
                    ),
                ],
              ),
            ),
            IconButton(tooltip: t.refresh, onPressed: _load, icon: const Icon(Icons.refresh_rounded, size: 20)),
          ],
        ),
        const SizedBox(height: AppSpacing.md),
        InlineHint(message: t.logsPrivacyHint, icon: Icons.lock_outline_rounded),
        const SizedBox(height: AppSpacing.md),
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: _logs.isEmpty
              ? AppCard(child: EmptyState(icon: Icons.receipt_long_outlined, title: t.noLogsTitle, message: t.noLogsMessage))
              : SettingsCardList(
                  children: <Widget>[
                    for (final log in _logs)
                      Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: <Widget>[
                          RequestRow(log: log, onTap: () => setState(() => _open = _open == log.id ? null : log.id)),
                          if (_open == log.id) _RequestDetails(log: log),
                        ],
                      ),
                  ],
                ),
        ),
      ],
    );
  }
}

/// The facts of one request in plain words, and the route it took.
class _RequestDetails extends StatelessWidget {
  const _RequestDetails({required this.log});

  final RequestLog log;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final facts = <(String, String)>[
      (t.detailRequested, log.requestedModel),
      if (log.keyName != null) (t.detailKey, log.keyName!),
      (t.detailResponse, formatMs(log.latencyMs)),
      if (log.ttftMs != null) (t.detailFirstToken, formatMs(log.ttftMs)),
      (t.detailTokens, t.tokensInOut(formatCount(log.inputTokens), formatCount(log.outputTokens))),
      (t.detailCost, log.isFree ? t.free : formatUsd(log.costUsd)),
      (t.detailFormat, '${log.apiFormat}${log.stream ? ' · stream' : ''}'),
      (t.detailTime, formatTime(log.createdAt)),
    ];
    return Container(
      color: palette.surfaceMuted,
      padding: const EdgeInsets.fromLTRB(48, 4, 20, 18),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          if (log.errorMessage != null) ...<Widget>[
            const SizedBox(height: 10),
            Text(log.errorMessage!, style: TextStyle(color: palette.error, fontSize: 13, height: 1.4)),
          ],
          const SizedBox(height: 10),
          Wrap(
            spacing: 32,
            runSpacing: 10,
            children: <Widget>[
              for (final (label, value) in facts)
                Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  mainAxisSize: MainAxisSize.min,
                  children: <Widget>[
                    Text(label, style: TextStyle(color: palette.textMuted, fontSize: 12)),
                    const SizedBox(height: 2),
                    Text(value, style: TextStyle(color: palette.text, fontSize: 13)),
                  ],
                ),
            ],
          ),
          if (log.trail.length > 1) ...<Widget>[
            const SizedBox(height: 16),
            Text(t.detailRoute, style: TextStyle(color: palette.textMuted, fontSize: 12)),
            const SizedBox(height: 6),
            for (final step in log.trail)
              Padding(
                padding: const EdgeInsets.only(bottom: 4),
                child: Row(
                  children: <Widget>[
                    ToneDot(tone: step['outcome'] == 'success' ? PillTone.success : PillTone.neutral, size: 6),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Text.rich(
                        TextSpan(
                          children: <InlineSpan>[
                            TextSpan(text: '${step['model']}', style: GoogleFonts.geistMono(fontSize: 12.5, color: palette.text)),
                            TextSpan(text: '  ${_outcome(t, '${step['outcome']}')}', style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
                          ],
                        ),
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  ],
                ),
              ),
          ],
        ],
      ),
    );
  }

  static String _outcome(AppL10n t, String outcome) => switch (outcome) {
        'success' => t.outcomeAnswered,
        'hedge_lost' => t.outcomeSlower,
        'rate_limit' || 'quota_exhausted' => t.outcomeBusy,
        _ => t.outcomeFailed(outcome.replaceAll('_', ' ')),
      };
}
