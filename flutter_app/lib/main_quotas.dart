import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';

String metricLabel(AppL10n t, String metric) => switch (metric) {
      'rpm' => t.metricRpm,
      'rpd' => t.metricRpd,
      'tpm' => t.metricTpm,
      'tpd' => t.metricTpd,
      'balance' || 'credits_usd' => t.balance,
      _ => metric,
    };

String sourceLabel(AppL10n t, String? source) => switch (source) {
      'header' => t.sourceHeader,
      'curated' => t.sourceCurated,
      'learned' => t.sourceLearned,
      'manual' => t.sourceManual,
      'error' || 'probe' => t.sourceError,
      _ => '',
    };

class QuotasScreen extends StatefulWidget {
  const QuotasScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<QuotasScreen> createState() => _QuotasScreenState();
}

class _QuotasScreenState extends State<QuotasScreen> {
  List<Map<String, dynamic>> _accounts = <Map<String, dynamic>>[];
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
      final accounts = await widget.controller.api.quotas();
      setState(() => _accounts = accounts);
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
        title: t.quotasTitle,
        description: t.quotasDescription,
        trailing: IconButton(tooltip: t.refresh, onPressed: _load, icon: const Icon(Icons.refresh)),
      ),
      children: <Widget>[
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: _accounts.isEmpty
              ? SectionCard(child: EmptyState(icon: Icons.data_usage_outlined, title: t.noProvidersTitle, message: t.noProvidersMessage))
              : Column(
                  children: <Widget>[
                    for (final group in _accounts) ...<Widget>[_AccountQuota(group: group), const SizedBox(height: AppSpacing.md)],
                  ],
                ),
        ),
      ],
    );
  }
}

class _AccountQuota extends StatelessWidget {
  const _AccountQuota({required this.group});

  final Map<String, dynamic> group;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final account = Map<String, dynamic>.from(group['account'] as Map);
    final pool = (group['pool'] as List).cast<Map>();
    final probes = (group['probes'] as List).cast<Map>();
    final models = (group['models'] as List).cast<Map>();
    final limited = models.where((model) => (model['windows'] as List).isNotEmpty).toList();
    final unlimitedFree = models.where((model) => (model['windows'] as List).isEmpty && model['isFree'] == true).length;
    return SectionCard(
      title: '${account['label']} · ${account['slug']}',
      trailing: MetaPill(
        label: account['billingMode'] == 'free_tier' ? t.free : t.paid,
        tone: account['billingMode'] == 'free_tier' ? PillTone.success : PillTone.accent,
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: <Widget>[
          if (probes.isNotEmpty) ...<Widget>[
            Wrap(
              spacing: 6,
              runSpacing: 6,
              children: <Widget>[
                for (final probe in probes)
                  MetaPill(
                    icon: Icons.account_balance_wallet_outlined,
                    label: '${metricLabel(t, probe['metric'] as String)}: ${probe['remaining'] ?? '—'}${probe['limit'] == null ? '' : ' / ${probe['limit']}'}',
                    tone: PillTone.info,
                  ),
              ],
            ),
            const SizedBox(height: AppSpacing.md),
          ],
          if (pool.isNotEmpty) ...<Widget>[
            Text(t.sharedLimits, style: const TextStyle(fontWeight: FontWeight.w700)),
            const SizedBox(height: AppSpacing.sm),
            for (final window in pool) _WindowBar(window: window),
            const SizedBox(height: AppSpacing.sm),
          ],
          if (limited.isNotEmpty) ...<Widget>[
            Text(t.perModelLimits, style: const TextStyle(fontWeight: FontWeight.w700)),
            const SizedBox(height: AppSpacing.sm),
            for (final model in limited)
              Padding(
                padding: const EdgeInsets.only(bottom: AppSpacing.md),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: <Widget>[
                    Row(
                      children: <Widget>[
                        Expanded(child: Text('${model['id']}', overflow: TextOverflow.ellipsis, style: const TextStyle(fontFamily: 'monospace', fontSize: 12.5))),
                        if (model['isFree'] == true) MetaPill(label: t.free, tone: PillTone.success),
                      ],
                    ),
                    const SizedBox(height: 6),
                    for (final window in (model['windows'] as List).cast<Map>()) _WindowBar(window: window),
                  ],
                ),
              ),
          ],
          if (pool.isEmpty && limited.isEmpty) Text(t.noQuotaData, style: TextStyle(color: palette.textMuted)),
          if (unlimitedFree > 0) Text(t.onlyFreeModels('$unlimitedFree'), style: TextStyle(color: palette.textMuted, fontSize: 12)),
        ],
      ),
    );
  }
}

class _WindowBar extends StatelessWidget {
  const _WindowBar({required this.window});

  final Map window;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final limit = (window['limit'] as num?)?.toDouble();
    final remaining = (window['remaining'] as num?)?.toDouble() ?? 0;
    final used = limit == null ? (window['used'] as num? ?? 0).toDouble() : (limit - remaining).clamp(0, limit).toDouble();
    final resetAt = window['resetAt'] as num?;
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Row(
        children: <Widget>[
          SizedBox(width: 130, child: Text(metricLabel(t, window['metric'] as String), style: TextStyle(color: palette.textSoft, fontSize: 12))),
          Expanded(child: UsageBar(fraction: limit == null || limit == 0 ? 1 : used / limit)),
          const SizedBox(width: AppSpacing.sm),
          SizedBox(
            width: 210,
            child: Text(
              [
                limit == null ? formatCount(used) : t.usedOf(formatCount(used), formatCount(limit)),
                sourceLabel(t, window['source'] as String?),
                if (resetAt != null) formatTime(DateTime.fromMillisecondsSinceEpoch(resetAt.toInt())),
              ].where((part) => part.isNotEmpty).join(' · '),
              textAlign: TextAlign.end,
              style: TextStyle(color: palette.textMuted, fontSize: 12),
            ),
          ),
        ],
      ),
    );
  }
}
