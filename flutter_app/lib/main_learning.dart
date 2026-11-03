import 'dart:math' as math;

import 'package:fl_chart/fl_chart.dart';
import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_routing.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';

double _num(Object? value) => value is num ? value.toDouble() : 0;
double? _maybe(Object? value) => value is num ? value.toDouble() : null;
String _percent(double value) => '${(value * 100).round()} %';

/// What routing learned from the user's own traffic: how often its first
/// choice answers, which model does which kind of work best, and what it found
/// out by itself. Every number comes from the same counts routing decides on.
class LearnedView extends StatefulWidget {
  const LearnedView({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<LearnedView> createState() => _LearnedViewState();
}

class _LearnedViewState extends State<LearnedView> {
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
      final data = await widget.controller.api.learning();
      if (mounted) setState(() => _data = data);
    } catch (error) {
      if (mounted) setState(() => _error = '$error');
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
    final answers = _num(data['answers']);
    final curve = ((data['curve'] as List?) ?? const <Object>[]).cast<Map>();
    if (answers == 0 && curve.isEmpty) {
      return AppCard(
        child: EmptyState(
          icon: Icons.auto_awesome_outlined,
          title: t.learnedEmptyTitle,
          message: t.learnedEmptyMessage,
          action: FilledButton(onPressed: () => widget.controller.go(AppPage.playground), child: Text(t.homeOpenPlayground)),
        ),
      );
    }
    final then = _maybe(data['firstTryThen']);
    final now = _maybe(data['firstTryNow']);
    final best = ((data['best'] as List?) ?? const <Object>[]).cast<Map>();
    final map = Map<String, dynamic>.from((data['map'] as Map?) ?? const <String, dynamic>{});
    final fastest = ((data['fastest'] as List?) ?? const <Object>[]).cast<Map>();
    final facts = ((data['discoveries'] as List?) ?? const <Object>[]).cast<Map>();
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        MetricStrip(
          metrics: <({String label, String value, String? caption})>[
            (label: t.learnedExperience, value: formatCount(answers), caption: t.learnedExperienceCaption),
            (label: t.learnedKnowHow, value: '${_num(data['tasksKnown']).toInt()}', caption: t.learnedKnowHowCaption('${_num(data['modelsKnown']).toInt()}')),
            (label: t.learnedRescued, value: formatCount(_num(data['rescued'])), caption: t.learnedRescuedCaption),
            (
              label: t.learnedFirstTry,
              value: now == null ? '—' : _percent(now),
              caption: then == null || now == null ? t.learnedFirstTryCaption : (now > then ? t.learnedUpFrom(_percent(then)) : t.learnedWas(_percent(then))),
            ),
          ],
        ),
        if (curve.length >= 3) ...<Widget>[
          const SizedBox(height: 28),
          SectionCard(
            title: t.learnedCurveTitle,
            trailing: _num(data['noCapacity']) > 0
                ? Text(t.learnedNoCapacity(formatCount(_num(data['noCapacity']))), style: TextStyle(fontSize: 12.5, color: neoRouterPaletteOf(context).textMuted))
                : null,
            child: _Curve(curve: curve),
          ),
        ],
        if (best.isNotEmpty) ...<Widget>[
          const SizedBox(height: 28),
          SectionCard(
            title: t.learnedMapTitle,
            child: _ExperienceMap(map: map, best: best),
          ),
          const SizedBox(height: 28),
          SettingsHeading(title: t.learnedBestTitle),
          _BestPerTask(best: best),
        ],
        if (fastest.isNotEmpty || facts.isNotEmpty) ...<Widget>[
          const SizedBox(height: 28),
          LayoutBuilder(
            builder: (context, constraints) {
              final speed = fastest.isEmpty ? null : _Fastest(rows: fastest);
              final found = facts.isEmpty ? null : _Discoveries(facts: facts);
              if (constraints.maxWidth < 860 || speed == null || found == null) {
                return Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: <Widget>[
                    ?found,
                    if (speed != null && found != null) const SizedBox(height: 28),
                    ?speed,
                  ],
                );
              }
              return Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Expanded(child: found),
                  const SizedBox(width: AppSpacing.lg),
                  Expanded(child: speed),
                ],
              );
            },
          ),
        ],
      ],
    );
  }
}

/// How often the first choice answered, per day.
class _Curve extends StatelessWidget {
  const _Curve({required this.curve});

  final List<Map> curve;

  @override
  Widget build(BuildContext context) {
    final palette = neoRouterPaletteOf(context);
    final style = TextStyle(color: palette.textMuted, fontSize: 11.5);
    final step = math.max(1, (curve.length / 6).ceil());
    String day(Map item) {
      final time = DateTime.fromMillisecondsSinceEpoch(_num(item['bucket']).toInt());
      return '${time.day.toString().padLeft(2, '0')}.${time.month.toString().padLeft(2, '0')}.';
    }

    return SizedBox(
      height: 210,
      child: TweenAnimationBuilder<double>(
        tween: Tween<double>(begin: 0, end: 1),
        duration: const Duration(milliseconds: 900),
        curve: Curves.easeOutCubic,
        builder: (context, progress, _) => LineChart(
          duration: Duration.zero,
          LineChartData(
            minX: 0,
            maxX: (curve.length - 1).toDouble(),
            minY: 0,
            maxY: 100,
            borderData: FlBorderData(show: false),
            gridData: FlGridData(drawVerticalLine: false, horizontalInterval: 25, getDrawingHorizontalLine: (_) => FlLine(color: palette.border, strokeWidth: 1)),
            titlesData: FlTitlesData(
              topTitles: const AxisTitles(),
              rightTitles: const AxisTitles(),
              leftTitles: AxisTitles(
                sideTitles: SideTitles(
                  showTitles: true,
                  interval: 25,
                  reservedSize: 44,
                  getTitlesWidget: (value, _) => Text('${value.toInt()} %', style: style),
                ),
              ),
              bottomTitles: AxisTitles(
                sideTitles: SideTitles(
                  showTitles: true,
                  interval: 1,
                  reservedSize: 24,
                  getTitlesWidget: (value, _) {
                    final index = value.toInt();
                    if (index < 0 || index >= curve.length || index % step != 0) return const SizedBox.shrink();
                    return Padding(padding: const EdgeInsets.only(top: 6), child: Text(day(curve[index]), style: style));
                  },
                ),
              ),
            ),
            lineTouchData: LineTouchData(
              touchTooltipData: LineTouchTooltipData(
                getTooltipColor: (_) => palette.text,
                getTooltipItems: (spots) => spots.map((spot) {
                  final item = curve[spot.x.toInt()];
                  return LineTooltipItem('${day(item)}  ${spot.y.round()} %', TextStyle(color: palette.scaffold, fontSize: 12));
                }).toList(),
              ),
            ),
            lineBarsData: <LineChartBarData>[
              LineChartBarData(
                spots: <FlSpot>[for (var index = 0; index < curve.length; index++) FlSpot(index.toDouble(), _num(curve[index]['rate']) * 100 * progress)],
                isCurved: true,
                preventCurveOverShooting: true,
                color: palette.accent,
                barWidth: 2.5,
                dotData: const FlDotData(show: false),
                belowBarData: BarAreaData(
                  show: true,
                  gradient: LinearGradient(
                    begin: Alignment.topCenter,
                    end: Alignment.bottomCenter,
                    colors: <Color>[palette.accent.withValues(alpha: 0.22), palette.accent.withValues(alpha: 0)],
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Kinds of work against models: each cell is one score for how good the model
/// is at that work, from benchmarks and experience. A red edge marks one that
/// often failed for this user, and the highest score per kind of work is ringed.
class _ExperienceMap extends StatelessWidget {
  const _ExperienceMap({required this.map, required this.best});

  final Map<String, dynamic> map;
  final List<Map> best;

  static const double _minCell = 64;
  static const double _rowHeight = 44;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final tasks = ((map['tasks'] as List?) ?? const <Object>[]).cast<String>();
    final models = ((map['models'] as List?) ?? const <Object>[]).cast<Map>();
    final cells = <String, Map>{for (final cell in ((map['cells'] as List?) ?? const <Object>[]).cast<Map>()) '${cell['task']}|${cell['model']}': cell};
    final winners = <String, String>{for (final item in best) '${item['task']}': '${item['model']}'};
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        Text(t.learnedMapHelp, style: TextStyle(fontSize: 12.5, height: 1.4, color: palette.textMuted)),
        const SizedBox(height: 6),
        LayoutBuilder(
          builder: (context, constraints) {
            // Task names get less room on a phone, where the map scrolls sideways.
            final labelWidth = constraints.maxWidth < 500 ? 104.0 : 150.0;
            final cellWidth = math.max(_minCell, (constraints.maxWidth - labelWidth) / math.max(1, models.length));
            final grid = SizedBox(
              width: labelWidth + cellWidth * models.length,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Row(
                    children: <Widget>[
                      SizedBox(width: labelWidth),
                      for (final model in models)
                        SizedBox(
                          width: cellWidth,
                          height: 40,
                          child: Tooltip(
                            message: '${model['name']}',
                            child: Align(
                              alignment: Alignment.bottomCenter,
                              child: Padding(
                                padding: const EdgeInsets.fromLTRB(3, 0, 3, 8),
                                child: Text(
                                  shortModelName('${model['name']}'),
                                  maxLines: 2,
                                  textAlign: TextAlign.center,
                                  overflow: TextOverflow.ellipsis,
                                  style: TextStyle(fontSize: 11, height: 1.2, color: palette.textSoft),
                                ),
                              ),
                            ),
                          ),
                        ),
                    ],
                  ),
                  for (final (row, task) in tasks.indexed)
                    Row(
                      children: <Widget>[
                        SizedBox(
                          width: labelWidth,
                          child: Text(
                            taskLabel(t, task),
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(fontSize: 13, fontWeight: FontWeight.w500, color: palette.text),
                          ),
                        ),
                        for (final (column, model) in models.indexed)
                          SizedBox(
                            width: cellWidth,
                            height: _rowHeight,
                            child: _Cell(
                              cell: cells['$task|${model['model']}'],
                              winner: winners[task] == model['model'],
                              // A diagonal wash as the map comes in.
                              delay: (row + column) * 45,
                              tooltipTitle: '${taskLabel(t, task)} · ${shortModelName('${model['name']}')}',
                            ),
                          ),
                      ],
                    ),
                ],
              ),
            );
            return SingleChildScrollView(scrollDirection: Axis.horizontal, child: grid);
          },
        ),
        const SizedBox(height: 14),
        Wrap(
          spacing: AppSpacing.md,
          runSpacing: 6,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            Row(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Text(t.learnedLegendWorse, style: TextStyle(fontSize: 12, color: palette.textMuted)),
                const SizedBox(width: 8),
                Container(
                  width: 96,
                  height: 6,
                  decoration: BoxDecoration(
                    borderRadius: BorderRadius.circular(AppRadius.pill),
                    gradient: LinearGradient(colors: <Color>[palette.accent.withValues(alpha: 0.1), palette.accent]),
                  ),
                ),
                const SizedBox(width: 8),
                Text(t.learnedLegendBetter, style: TextStyle(fontSize: 12, color: palette.textMuted)),
              ],
            ),
            Row(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Container(
                  width: 12,
                  height: 12,
                  decoration: BoxDecoration(borderRadius: BorderRadius.circular(3), border: Border.all(color: palette.text, width: 1.4)),
                ),
                const SizedBox(width: 6),
                Text(t.learnedLegendBest, style: TextStyle(fontSize: 12, color: palette.textMuted)),
              ],
            ),
            Row(
              mainAxisSize: MainAxisSize.min,
              children: <Widget>[
                Container(
                  width: 12,
                  height: 12,
                  decoration: BoxDecoration(borderRadius: BorderRadius.circular(3), border: Border.all(color: palette.error, width: 1.4)),
                ),
                const SizedBox(width: 6),
                Text(t.learnedLegendFails, style: TextStyle(fontSize: 12, color: palette.textMuted)),
              ],
            ),
            Text(t.learnedLegendEstimated, style: TextStyle(fontSize: 12, color: palette.textMuted)),
          ],
        ),
      ],
    );
  }
}

class _Cell extends StatelessWidget {
  const _Cell({required this.cell, required this.winner, required this.delay, required this.tooltipTitle});

  final Map? cell;
  final bool winner;
  final int delay;
  final String tooltipTitle;

  // Below this share of usable answers a model gets the red edge.
  static const double _failsOften = 0.8;

  Color _color(NeoRouterPalette palette, double score) => palette.accent.withValues(alpha: 0.06 + math.pow(score, 1.3) * 0.84);

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final item = cell;
    if (item == null) {
      return Center(
        child: Container(
          width: 4,
          height: 4,
          decoration: BoxDecoration(color: palette.border, shape: BoxShape.circle),
        ),
      );
    }
    final rate = _num(item['rate']);
    String points(Object? value) => '${(_num(value) * 100).round()}';
    final score = points(item['score']);
    final benchmarked = item['benchmarked'] == true;
    final fill = _color(palette, _num(item['score']));
    // Dark or light numbers by how bright the cell really is, in either theme.
    final ink = Color.alphaBlend(fill, palette.surface).computeLuminance() > 0.3 ? const Color(0xFF171F1A) : palette.text;
    final total = 500 + delay;
    final liked = _num(item['liked']).toInt();
    final disliked = _num(item['disliked']).toInt();
    return Tooltip(
      message: <String>[
        tooltipTitle,
        t.learnedCellScore(score),
        benchmarked ? t.learnedCellBenchmark(points(item['quality'])) : t.learnedCellEstimate(points(item['quality'])),
        t.learnedCellSpeed(points(item['speed'])),
        t.learnedCellLine(_percent(rate), formatCount(_num(item['answers']))),
        if (liked + disliked > 0) t.learnedCellVotes('$liked', '$disliked'),
      ].join('\n'),
      child: TweenAnimationBuilder<double>(
        tween: Tween<double>(begin: 0, end: 1),
        duration: Duration(milliseconds: total),
        curve: Interval(delay / total, 1, curve: Curves.easeOutCubic),
        builder: (context, progress, child) => Opacity(
          opacity: progress,
          child: Transform.scale(scale: 0.86 + 0.14 * progress, child: child),
        ),
        child: Container(
          margin: const EdgeInsets.all(2),
          alignment: Alignment.center,
          decoration: BoxDecoration(
            color: fill,
            borderRadius: BorderRadius.circular(AppRadius.tag),
            border: winner
                ? Border.all(color: palette.text, width: 1.4)
                : rate < _failsOften
                ? Border.all(color: palette.error, width: 1.4)
                : null,
          ),
          child: Text(
            benchmarked ? score : '~$score',
            style: TextStyle(fontSize: 12.5, fontWeight: winner ? FontWeight.w700 : FontWeight.w500, color: ink),
          ),
        ),
      ),
    );
  }
}

/// One card per kind of work, naming the model that has done best at it.
class _BestPerTask extends StatelessWidget {
  const _BestPerTask({required this.best});

  final List<Map> best;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return LayoutBuilder(
      builder: (context, constraints) {
        final columns = constraints.maxWidth < 560 ? 1 : (constraints.maxWidth < 900 ? 2 : 3);
        final width = (constraints.maxWidth - AppSpacing.sm * (columns - 1)) / columns;
        return Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          children: <Widget>[
            for (final item in best)
              SizedBox(
                width: width,
                child: AppCard(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      Text(taskLabel(t, '${item['task']}'), style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
                      const SizedBox(height: 6),
                      Text(
                        shortModelName('${item['name']}'),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: palette.text),
                      ),
                      const SizedBox(height: 10),
                      ClipRRect(
                        borderRadius: BorderRadius.circular(AppRadius.pill),
                        child: LinearProgressIndicator(
                          minHeight: 4,
                          value: _num(item['score']),
                          backgroundColor: palette.surfaceRaised,
                          valueColor: AlwaysStoppedAnimation<Color>(palette.accent),
                        ),
                      ),
                      const SizedBox(height: 8),
                      Text(
                        t.learnedBestLine(
                          '${item['benchmarked'] == true ? '' : '~'}${(_num(item['score']) * 100).round()}',
                          _percent(_num(item['rate'])),
                          formatCount(_num(item['modelAnswers'])),
                        ),
                        style: TextStyle(fontSize: 12, color: palette.textMuted),
                      ),
                    ],
                  ),
                ),
              ),
          ],
        );
      },
    );
  }
}

class _Fastest extends StatelessWidget {
  const _Fastest({required this.rows});

  final List<Map> rows;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(title: t.learnedFastestTitle),
        SettingsCardList(
          children: <Widget>[
            for (final row in rows)
              Padding(
                padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 13),
                child: Row(
                  children: <Widget>[
                    Expanded(
                      child: Text(
                        shortModelName('${row['name']}'),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w500, color: palette.text),
                      ),
                    ),
                    if (_maybe(row['reliability']) != null) ...<Widget>[
                      Text(t.learnedWorked(_percent(_num(row['reliability']))), style: TextStyle(fontSize: 12.5, color: palette.textMuted)),
                      const SizedBox(width: AppSpacing.sm),
                    ],
                    Text(formatMs(_maybe(row['latencyMs'])), style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600, color: palette.accent)),
                  ],
                ),
              ),
          ],
        ),
      ],
    );
  }
}

/// What nobody told it: limits, room a model needs, models it is resting.
class _Discoveries extends StatelessWidget {
  const _Discoveries({required this.facts});

  final List<Map> facts;

  String _limit(AppL10n t, Map fact) {
    final value = '${_num(fact['value']).toInt()}';
    final name = shortModelName('${fact['name']}');
    return switch ('${fact['metric']}') {
      'rpm' => t.learnedLimitRpm(name, value),
      'rpd' => t.learnedLimitRpd(name, value),
      'tpm' => t.learnedLimitTpm(name, value),
      _ => t.learnedLimitTpd(name, value),
    };
  }

  String _reason(AppL10n t, String? reason) => switch (reason) {
    'rate_limited' || 'quota_exhausted' || 'no_free_quota' => t.learnedReasonLimit,
    'auth_failed' || 'payment_required' => t.learnedReasonAccount,
    'not_available' => t.learnedReasonMissing,
    'incompatible' => t.learnedReasonIncompatible,
    _ => t.learnedReasonFailing,
  };

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: <Widget>[
        SettingsHeading(title: t.learnedFoundTitle),
        SettingsCardList(
          children: <Widget>[
            for (final fact in facts)
              Builder(
                builder: (context) {
                  final kind = '${fact['kind']}';
                  final name = shortModelName('${fact['name']}');
                  final at = _maybe(fact['at']);
                  final title = switch (kind) {
                    'limit' => _limit(t, fact),
                    'room' => t.learnedRoom(name, '${_num(fact['value']).toInt()}'),
                    _ => t.learnedResting(name, formatTime(DateTime.fromMillisecondsSinceEpoch(_num(fact['until']).toInt()))),
                  };
                  final detail = kind == 'resting' ? _reason(t, fact['reason'] as String?) : (at == null ? null : formatAgo(t, DateTime.fromMillisecondsSinceEpoch(at.toInt())));
                  return Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
                    child: Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: <Widget>[
                        Icon(
                          switch (kind) {
                            'limit' => Icons.speed_rounded,
                            'room' => Icons.unfold_more_rounded,
                            _ => Icons.bedtime_outlined,
                          },
                          size: 17,
                          color: kind == 'resting' ? palette.accent : palette.textMuted,
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: <Widget>[
                              Text(title, style: TextStyle(fontSize: 13.5, color: palette.text)),
                              if (detail != null) Text(detail, style: TextStyle(fontSize: 12, color: palette.textMuted)),
                            ],
                          ),
                        ),
                      ],
                    ),
                  );
                },
              ),
          ],
        ),
      ],
    );
  }
}
