import 'package:flutter/material.dart';
import '../l10n/gen/app_localizations.dart';
import '../main_shared.dart';
import '../main_theme.dart';
import 'charts.dart';
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
    final colors = [palette.accent, palette.textSoft];
    // Two models: only the tests both took. One model: all of its tests.
    final metrics = models.length == 2
        ? models.first.scores
              .where(
                (score) =>
                    models.last.scores.any((other) => other.id == score.id),
              )
              .toList()
        : models.first.scores;
    String sourceName(String id) =>
        sources.where((source) => source.id == id).firstOrNull?.name ?? id;
    return AppCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              for (var i = 0; i < models.length; i++) ...[
                if (i > 0) const SizedBox(width: 12),
                Expanded(
                  child: Container(
                    padding: const EdgeInsets.fromLTRB(12, 8, 4, 8),
                    decoration: BoxDecoration(
                      border: Border(
                        left: BorderSide(color: colors[i], width: 3),
                      ),
                    ),
                    child: Row(
                      children: [
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                models[i].name,
                                maxLines: 1,
                                overflow: TextOverflow.ellipsis,
                                style: TextStyle(
                                  color: palette.textSoft,
                                  fontSize: 12,
                                ),
                              ),
                              Text(
                                models[i].score.toStringAsFixed(1),
                                style: TextStyle(
                                  fontSize: 24,
                                  fontWeight: FontWeight.w500,
                                  color: palette.text,
                                ),
                              ),
                            ],
                          ),
                        ),
                        IconButton(
                          visualDensity: VisualDensity.compact,
                          tooltip: t.discoveryRemoveCompare(models[i].name),
                          onPressed: () => onRemove(models[i]),
                          icon: const Icon(Icons.close, size: 16),
                        ),
                      ],
                    ),
                  ),
                ),
              ],
              if (models.length == 1) ...[
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    t.discoveryCompareHint,
                    style: TextStyle(color: palette.textMuted, fontSize: 12),
                  ),
                ),
              ],
            ],
          ),
          const SizedBox(height: 12),
          if (metrics.isEmpty)
            Text(
              t.discoveryNoShared,
              style: TextStyle(color: palette.textMuted, fontSize: 12),
            )
          else
            ComparisonChart(
              models: models,
              metrics: metrics,
              label: (metric) =>
                  '${sourceName(metric.sourceId)} · ${metricName(metric.metric)}',
            ),
        ],
      ),
    );
  }
}
