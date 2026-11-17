import 'package:flutter/material.dart';
import '../l10n/gen/app_localizations.dart';
import '../main_shared.dart';
import '../main_theme.dart';
import 'models.dart';
import 'widgets.dart';

class BenchmarkComparison extends StatelessWidget {
  const BenchmarkComparison({
    super.key,
    required this.models,
    required this.sources,
    required this.onRemove,
  });
  final List<BenchmarkModel> models;
  final List<BenchmarkSource> sources;
  final ValueChanged<BenchmarkModel> onRemove;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final metrics = <String, BenchmarkMetric>{};
    for (final model in models) {
      for (final metric in model.scores) {
        metrics[metric.id] = metric;
      }
    }
    final shared = models.length == 2
        ? models.first.scores
              .where(
                (score) =>
                    models.last.scores.any((other) => other.id == score.id),
              )
              .length
        : 0;
    String sourceName(String id) =>
        sources.where((source) => source.id == id).firstOrNull?.name ?? id;
    return SectionCard(
      title: t.discoveryCompare,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            t.discoveryCompareHint,
            style: TextStyle(color: palette.textSoft),
          ),
          if (models.isNotEmpty) ...[
            const SizedBox(height: 16),
            LayoutBuilder(
              builder: (context, constraints) {
                Widget modelCard(BenchmarkModel model) => Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(
                            model.name,
                            style: TextStyle(
                              fontWeight: FontWeight.w600,
                              color: palette.text,
                            ),
                          ),
                        ),
                        IconButton(
                          tooltip: t.discoveryRemoveCompare(model.name),
                          onPressed: () => onRemove(model),
                          icon: const Icon(Icons.close, size: 18),
                        ),
                      ],
                    ),
                    Text(
                      '${model.score.toStringAsFixed(1)} / 100',
                      style: TextStyle(
                        fontSize: 28,
                        fontWeight: FontWeight.w500,
                        color: palette.accent,
                      ),
                    ),
                    Text(
                      t.discoveryCoverage(
                        '${model.sourceCount}',
                        '${model.scores.length}',
                      ),
                      style: TextStyle(color: palette.textMuted, fontSize: 12),
                    ),
                    const SizedBox(height: 12),
                    BenchmarkScoreBar(score: model.score, selected: true),
                  ],
                );
                if (constraints.maxWidth < 500) {
                  return Column(
                    children: [
                      for (final model in models)
                        Padding(
                          padding: const EdgeInsets.only(bottom: 16),
                          child: modelCard(model),
                        ),
                    ],
                  );
                }
                return Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    for (var i = 0; i < models.length; i++) ...[
                      if (i > 0) const SizedBox(width: 32),
                      Expanded(child: modelCard(models[i])),
                    ],
                  ],
                );
              },
            ),
          ],
          if (models.length == 2) ...[
            const SizedBox(height: 20),
            Text(
              t.discoveryShared('$shared'),
              style: TextStyle(color: palette.textSoft),
            ),
            if (shared == 0)
              Padding(
                padding: const EdgeInsets.only(top: 8),
                child: Text(
                  t.discoveryNoShared,
                  style: TextStyle(color: palette.textMuted),
                ),
              ),
            const SizedBox(height: 8),
            ExpansionTile(
              tilePadding: EdgeInsets.zero,
              childrenPadding: EdgeInsets.zero,
              title: Text(t.discoveryBreakdown),
              children: [
                for (final metric in metrics.values)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 12),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.stretch,
                      children: [
                        Text(
                          '${sourceName(metric.sourceId)} · ${metricName(metric.metric)}',
                          style: TextStyle(
                            fontSize: 12,
                            color: palette.textSoft,
                          ),
                        ),
                        const SizedBox(height: 8),
                        Row(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            for (var i = 0; i < models.length; i++) ...[
                              if (i > 0) const SizedBox(width: 24),
                              Expanded(
                                child: _MetricValue(
                                  metric: models[i].scores
                                      .where((score) => score.id == metric.id)
                                      .firstOrNull,
                                ),
                              ),
                            ],
                          ],
                        ),
                        const SizedBox(height: 6),
                        Text(
                          t.discoveryTestScale('${metric.peers}'),
                          style: TextStyle(
                            color: palette.textMuted,
                            fontSize: 11,
                          ),
                        ),
                      ],
                    ),
                  ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

class _MetricValue extends StatelessWidget {
  const _MetricValue({this.metric});
  final BenchmarkMetric? metric;
  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(metric == null ? '—' : metric!.value.toStringAsFixed(3)),
      const SizedBox(height: 5),
      if (metric != null) BenchmarkScoreBar(score: metric!.percentile),
    ],
  );
}
