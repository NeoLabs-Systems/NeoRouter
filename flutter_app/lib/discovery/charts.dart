import 'dart:math' as math;
import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';
import '../l10n/gen/app_localizations.dart';
import '../main_theme.dart';
import 'models.dart';

Duration _motion(BuildContext context) =>
    MediaQuery.disableAnimationsOf(context)
    ? Duration.zero
    : const Duration(milliseconds: 450);

/// Ranked score bars with the value on top and the name below, like a
/// leaderboard. Free models carry the accent; the rest stay neutral.
class ScoreBarChart<T extends RankedBenchmark> extends StatelessWidget {
  const ScoreBarChart({
    super.key,
    required this.models,
    required this.selected,
    required this.onTap,
  });
  final List<T> models;
  final Set<String> selected;
  final ValueChanged<T> onTap;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final top = models.take(20).toList();
    Color colorOf(T model) => selected.contains(model.id)
        ? palette.accentStrong
        : model.isFree
        ? palette.accent
        : palette.textMuted.withValues(alpha: .45);
    return LayoutBuilder(
      builder: (context, constraints) {
        final width = math.max(constraints.maxWidth, top.length * 34.0);
        final chart = SizedBox(
          width: width,
          height: 300,
          child: BarChart(
            duration: _motion(context),
            BarChartData(
              maxY: 100,
              minY: 0,
              alignment: BarChartAlignment.spaceAround,
              gridData: FlGridData(
                drawVerticalLine: false,
                horizontalInterval: 25,
                getDrawingHorizontalLine: (_) => FlLine(
                  color: palette.border,
                  strokeWidth: 1,
                  dashArray: [3, 4],
                ),
              ),
              borderData: FlBorderData(show: false),
              barTouchData: BarTouchData(
                handleBuiltInTouches: true,
                touchCallback: (event, response) {
                  final index = response?.spot?.touchedBarGroupIndex;
                  if (event is FlTapUpEvent && index != null) onTap(top[index]);
                },
                touchTooltipData: BarTouchTooltipData(
                  getTooltipColor: (_) => palette.surfaceRaised,
                  getTooltipItem: (group, _, rod, _) => BarTooltipItem(
                    '${top[group.x].name}\n${rod.toY.toStringAsFixed(1)}',
                    TextStyle(color: palette.text, fontSize: 12),
                  ),
                ),
              ),
              titlesData: FlTitlesData(
                leftTitles: const AxisTitles(),
                rightTitles: const AxisTitles(),
                topTitles: AxisTitles(
                  sideTitles: SideTitles(
                    showTitles: true,
                    reservedSize: 20,
                    getTitlesWidget: (value, _) => Text(
                      top[value.toInt()].score.round().toString(),
                      style: TextStyle(color: palette.textSoft, fontSize: 11),
                    ),
                  ),
                ),
                bottomTitles: AxisTitles(
                  sideTitles: SideTitles(
                    showTitles: true,
                    reservedSize: 92,
                    getTitlesWidget: (value, meta) => SideTitleWidget(
                      meta: meta,
                      space: 6,
                      child: Transform.rotate(
                        angle: -math.pi / 4,
                        alignment: Alignment.topRight,
                        child: SizedBox(
                          width: 110,
                          child: Text(
                            top[value.toInt()].name,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            textAlign: TextAlign.right,
                            style: TextStyle(
                              color: palette.textSoft,
                              fontSize: 11,
                            ),
                          ),
                        ),
                      ),
                    ),
                  ),
                ),
              ),
              barGroups: [
                for (var i = 0; i < top.length; i++)
                  BarChartGroupData(
                    x: i,
                    barRods: [
                      BarChartRodData(
                        toY: top[i].score,
                        width: 18,
                        color: colorOf(top[i]),
                        borderRadius: const BorderRadius.vertical(
                          top: Radius.circular(4),
                        ),
                      ),
                    ],
                  ),
              ],
            ),
          ),
        );
        if (width <= constraints.maxWidth) return chart;
        return SingleChildScrollView(
          scrollDirection: Axis.horizontal,
          child: chart,
        );
      },
    );
  }
}

/// Score against price on a log scale. The models nothing beats on both
/// (cheaper and better) form the frontier and are drawn in the accent.
class ScoreCostChart extends StatelessWidget {
  const ScoreCostChart({super.key, required this.models});
  final List<BenchmarkModel> models;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final priced = models.where((model) => model.price != null).toList()
      ..sort((a, b) => a.price!.compareTo(b.price!));
    if (priced.length < 2) {
      return SizedBox(
        height: 120,
        child: Center(
          child: Text(
            t.discoveryNoPrices,
            style: TextStyle(color: palette.textMuted),
          ),
        ),
      );
    }
    final frontier = <String>{};
    var best = -1.0;
    for (final model in priced) {
      if (model.score > best) {
        best = model.score;
        frontier.add(model.id);
      }
    }
    double x(BenchmarkModel model) => math.log(model.price!) / math.ln10;
    final minX = x(priced.first).floorToDouble();
    final maxX = x(priced.last).ceilToDouble();
    final line = priced.where((model) => frontier.contains(model.id)).toList();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SizedBox(
          height: 300,
          child: Stack(
            children: [
              LineChart(
                duration: _motion(context),
                LineChartData(
                  minX: minX,
                  maxX: maxX == minX ? minX + 1 : maxX,
                  minY: 0,
                  maxY: 100,
                  lineTouchData: const LineTouchData(enabled: false),
                  gridData: const FlGridData(show: false),
                  borderData: FlBorderData(show: false),
                  titlesData: const FlTitlesData(show: false),
                  lineBarsData: [
                    LineChartBarData(
                      spots: [for (final m in line) FlSpot(x(m), m.score)],
                      color: palette.accent.withValues(alpha: .7),
                      barWidth: 1.5,
                      dashArray: [2, 4],
                      dotData: const FlDotData(show: false),
                    ),
                  ],
                ),
              ),
              ScatterChart(
                duration: _motion(context),
                ScatterChartData(
                  minX: minX,
                  maxX: maxX == minX ? minX + 1 : maxX,
                  minY: 0,
                  maxY: 100,
                  borderData: FlBorderData(show: false),
                  gridData: FlGridData(
                    verticalInterval: 1,
                    horizontalInterval: 25,
                    getDrawingHorizontalLine: (_) => FlLine(
                      color: palette.border,
                      strokeWidth: 1,
                      dashArray: [3, 4],
                    ),
                    getDrawingVerticalLine: (_) => FlLine(
                      color: palette.border,
                      strokeWidth: 1,
                      dashArray: [3, 4],
                    ),
                  ),
                  titlesData: FlTitlesData(
                    topTitles: const AxisTitles(),
                    rightTitles: const AxisTitles(),
                    leftTitles: AxisTitles(
                      sideTitles: SideTitles(
                        showTitles: true,
                        interval: 25,
                        reservedSize: 28,
                        getTitlesWidget: (value, _) => Text(
                          value.round().toString(),
                          style: TextStyle(
                            color: palette.textMuted,
                            fontSize: 11,
                          ),
                        ),
                      ),
                    ),
                    bottomTitles: AxisTitles(
                      sideTitles: SideTitles(
                        showTitles: true,
                        interval: 1,
                        reservedSize: 22,
                        getTitlesWidget: (value, _) {
                          final price = math.pow(10, value).toDouble();
                          return Text(
                            price < 1
                                ? '\$${price.toStringAsFixed(price < .1 ? 2 : 1)}'
                                : '\$${price.round()}',
                            style: TextStyle(
                              color: palette.textMuted,
                              fontSize: 11,
                            ),
                          );
                        },
                      ),
                    ),
                  ),
                  scatterTouchData: ScatterTouchData(
                    touchTooltipData: ScatterTouchTooltipData(
                      getTooltipColor: (_) => palette.surfaceRaised,
                      getTooltipItems: (spot) {
                        final model = priced.firstWhere(
                          (m) => x(m) == spot.x && m.score == spot.y,
                        );
                        return ScatterTooltipItem(
                          '${model.name}\n${model.score.toStringAsFixed(1)} · \$${model.price!.toStringAsFixed(2)}',
                          textStyle: TextStyle(
                            color: palette.text,
                            fontSize: 12,
                          ),
                        );
                      },
                    ),
                  ),
                  scatterSpots: [
                    for (final model in priced)
                      ScatterSpot(
                        x(model),
                        model.score,
                        dotPainter: FlDotCirclePainter(
                          radius: frontier.contains(model.id) ? 6 : 4.5,
                          color: frontier.contains(model.id)
                              ? palette.accent
                              : palette.textMuted.withValues(alpha: .5),
                        ),
                      ),
                  ],
                ),
              ),
            ],
          ),
        ),
        const SizedBox(height: 6),
        Text(
          t.discoveryPriceAxis,
          textAlign: TextAlign.center,
          style: TextStyle(color: palette.textMuted, fontSize: 11),
        ),
      ],
    );
  }
}

/// Two models side by side per shared test, as paired percentile bars.
class ComparisonChart extends StatelessWidget {
  const ComparisonChart({
    super.key,
    required this.models,
    required this.metrics,
    required this.label,
  });
  final List<BenchmarkModel> models;
  final List<BenchmarkMetric> metrics;
  final String Function(BenchmarkMetric metric) label;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final colors = [palette.accent, palette.textSoft];
    return Column(
      children: [
        for (final metric in metrics)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 5),
            child: Row(
              children: [
                SizedBox(
                  width: 150,
                  child: Text(
                    label(metric),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: palette.textSoft, fontSize: 12),
                  ),
                ),
                Expanded(
                  child: Column(
                    children: [
                      for (var i = 0; i < models.length; i++)
                        Padding(
                          padding: const EdgeInsets.symmetric(vertical: 1.5),
                          child: _Bar(
                            value: models[i].scores
                                .where((score) => score.id == metric.id)
                                .firstOrNull
                                ?.percentile,
                            color: colors[i],
                          ),
                        ),
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

class _Bar extends StatelessWidget {
  const _Bar({required this.value, required this.color});
  final double? value;
  final Color color;
  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    return Row(
      children: [
        Expanded(
          child: TweenAnimationBuilder<double>(
            tween: Tween(begin: 0, end: (value ?? 0) / 100),
            duration: _motion(context),
            curve: Curves.easeOutCubic,
            builder: (context, fraction, _) => Container(
              height: 7,
              decoration: BoxDecoration(
                color: palette.surfaceRaised,
                borderRadius: BorderRadius.circular(99),
              ),
              alignment: Alignment.centerLeft,
              child: FractionallySizedBox(
                widthFactor: fraction.clamp(0, 1),
                child: Container(
                  decoration: BoxDecoration(
                    color: color,
                    borderRadius: BorderRadius.circular(99),
                  ),
                ),
              ),
            ),
          ),
        ),
        SizedBox(
          width: 34,
          child: Text(
            value == null ? '—' : value!.round().toString(),
            textAlign: TextAlign.right,
            style: TextStyle(color: palette.textMuted, fontSize: 11),
          ),
        ),
      ],
    );
  }
}
