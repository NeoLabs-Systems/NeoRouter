import 'package:flutter/material.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/models.dart';

enum _Filter { all, free, enabled, tools, vision, issues }

String freeReasonLabel(AppL10n t, String? reason) => switch (reason) {
      'manual_free' => t.freeReasonManualFree,
      'manual_paid' => t.freeReasonManualPaid,
      'provider_free_model' => t.freeReasonProviderFreeModel,
      'listed_zero_price' => t.freeReasonListedZeroPrice,
      'free_tier_account' => t.freeReasonFreeTierAccount,
      'price_unknown' => t.freeReasonPriceUnknown,
      _ => t.freeReasonPaid,
    };

String routingReasonLabel(AppL10n t, String? reason) {
  final value = reason ?? '';
  if (value.startsWith('quota_')) return '${t.reasonQuota} (${value.substring(6)})';
  return switch (value) {
    'not_free' => t.reasonNotFree,
    'no_tools' => t.reasonNoTools,
    'no_vision' => t.reasonNoVision,
    'context_too_small' => t.reasonContext,
    'cooldown' => t.reasonCooldown,
    'circuit_open' => t.reasonCircuit,
    'rate_limited' => t.reasonRateLimited,
    'quota_exhausted' => t.reasonQuota,
    'budget_exhausted' => t.reasonBudget,
    'price_unknown' => t.reasonPriceUnknown,
    'model_disabled' || 'model_unavailable' => t.reasonDisabled,
    'account_unavailable' => t.reasonAccount,
    'auth_failed' => t.reasonAuth,
    'not_available' => t.reasonNotAvailable,
    'payment_required' => t.reasonPayment,
    'no_free_quota' => t.reasonNoFreeQuota,
    _ => value,
  };
}

class ModelsScreen extends StatefulWidget {
  const ModelsScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<ModelsScreen> createState() => _ModelsScreenState();
}

class _ModelsScreenState extends State<ModelsScreen> {
  List<CatalogModel> _models = <CatalogModel>[];
  bool _loading = true;
  String? _error;
  String _query = '';
  _Filter _filter = _Filter.all;
  String? _provider;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final models = await widget.controller.api.models();
      setState(() => _models = models);
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Iterable<CatalogModel> get _visible {
    final query = _query.toLowerCase();
    return _models.where((model) {
      if (_provider != null && model.providerSlug != _provider) return false;
      if (query.isNotEmpty && !model.id.toLowerCase().contains(query) && !model.name.toLowerCase().contains(query)) return false;
      return switch (_filter) {
        _Filter.all => true,
        _Filter.free => model.isFree,
        _Filter.enabled => model.enabled,
        _Filter.tools => model.tools == true,
        _Filter.vision => model.vision == true,
        _Filter.issues => !model.available || model.penalty >= 1,
      };
    });
  }

  Future<void> _toggle(CatalogModel model, bool enabled) async {
    try {
      final updated = await widget.controller.api.updateModel(model.ref, enabled: enabled);
      setState(() => _models = <CatalogModel>[for (final item in _models) item.ref == updated.ref ? updated : item]);
    } on ApiException catch (error) {
      if (mounted) showSnack(context, error.message);
    }
  }

  Future<void> _bulk({required bool enabled, bool onlyFree = false}) async {
    final accountId = _provider == null ? null : _models.firstWhere((model) => model.providerSlug == _provider).accountId;
    if (onlyFree) await widget.controller.api.bulkEnable(accountId: accountId, enabled: false);
    await widget.controller.api.bulkEnable(accountId: accountId, enabled: enabled, onlyFree: onlyFree);
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final providers = _models.map((model) => model.providerSlug).toSet().toList()..sort();
    final visible = _visible.toList();
    return PageScaffold(
      header: ScreenHeader(
        title: t.modelsTitle,
        description: t.modelsDescription,
        trailing: PopupMenuButton<String>(
          icon: const Icon(Icons.more_horiz),
          onSelected: (value) => switch (value) {
            'enable' => _bulk(enabled: true),
            'disable' => _bulk(enabled: false),
            _ => _bulk(enabled: true, onlyFree: true),
          },
          itemBuilder: (_) => <PopupMenuEntry<String>>[
            PopupMenuItem<String>(value: 'enable', child: Text(t.enableAll)),
            PopupMenuItem<String>(value: 'disable', child: Text(t.disableAll)),
            PopupMenuItem<String>(value: 'free', child: Text(t.enableFreeOnly)),
          ],
        ),
      ),
      children: <Widget>[
        Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: <Widget>[
            SizedBox(
              width: 280,
              child: TextField(
                decoration: InputDecoration(prefixIcon: const Icon(Icons.search), hintText: t.searchModels, isDense: true),
                onChanged: (value) => setState(() => _query = value),
              ),
            ),
            DropdownButton<String?>(
              value: _provider,
              underline: const SizedBox.shrink(),
              items: <DropdownMenuItem<String?>>[
                DropdownMenuItem<String?>(value: null, child: Text(t.filterAll)),
                for (final provider in providers) DropdownMenuItem<String?>(value: provider, child: Text(provider)),
              ],
              onChanged: (value) => setState(() => _provider = value),
            ),
            for (final filter in _Filter.values)
              ChoiceChip(
                label: Text(switch (filter) {
                  _Filter.all => t.filterAll,
                  _Filter.free => t.filterFree,
                  _Filter.enabled => t.filterEnabled,
                  _Filter.tools => t.filterTools,
                  _Filter.vision => t.filterVision,
                  _Filter.issues => t.filterIssues,
                }),
                selected: _filter == filter,
                onSelected: (_) => setState(() => _filter = filter),
              ),
            Text(t.modelsShown('${visible.length}'), style: TextStyle(color: neoRouterPaletteOf(context).textMuted, fontSize: 12)),
          ],
        ),
        AsyncBody(
          loading: _loading,
          error: _error,
          onRetry: _load,
          child: visible.isEmpty
              ? SectionCard(child: EmptyState(icon: Icons.view_list_outlined, title: t.noModelsTitle, message: t.noModelsMessage))
              : SectionCard(
                  padding: const EdgeInsets.symmetric(vertical: AppSpacing.xs),
                  child: Column(
                    children: <Widget>[
                      for (var index = 0; index < visible.length; index++) ...<Widget>[
                        if (index > 0) Divider(height: 1, color: neoRouterPaletteOf(context).border),
                        _ModelRow(
                          model: visible[index],
                          onToggle: (value) => _toggle(visible[index], value),
                          onOpen: () async {
                            final updated = await showDialog<CatalogModel>(
                              context: context,
                              builder: (_) => _ModelDialog(controller: widget.controller, model: visible[index]),
                            );
                            if (updated != null) await _load();
                          },
                        ),
                      ],
                    ],
                  ),
                ),
        ),
      ],
    );
  }
}

class _ModelRow extends StatelessWidget {
  const _ModelRow({required this.model, required this.onToggle, required this.onOpen});

  final CatalogModel model;
  final ValueChanged<bool> onToggle;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final wide = MediaQuery.sizeOf(context).width >= AppBreakpoints.tablet;
    final price = model.isFree
        ? MetaPill(label: t.free, tone: PillTone.success, icon: Icons.savings_outlined)
        : model.priceIn == null
            ? MetaPill(label: t.priceUnknown, tone: PillTone.warning)
            : MetaPill(label: t.pricePerMillion(formatPricePerMillion(model.priceIn), formatPricePerMillion(model.priceOut)), tone: PillTone.accent);
    final meters = Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        ScoreMeter(label: t.intelligence, value: model.intelligence),
        const SizedBox(width: AppSpacing.sm),
        ScoreMeter(label: t.speed, value: model.speedScore),
        const SizedBox(width: AppSpacing.sm),
        ScoreMeter(label: t.reliability, value: model.reliability),
      ],
    );
    return InkWell(
      onTap: onOpen,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: AppSpacing.md, vertical: AppSpacing.sm),
        child: Row(
          children: <Widget>[
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(model.name, overflow: TextOverflow.ellipsis, style: TextStyle(fontWeight: FontWeight.w700, color: model.enabled ? palette.text : palette.textMuted)),
                  const SizedBox(height: 2),
                  SelectableText(model.id, style: TextStyle(color: palette.textMuted, fontSize: 12, fontFamily: 'monospace')),
                  const SizedBox(height: 6),
                  Wrap(
                    spacing: 6,
                    runSpacing: 6,
                    children: <Widget>[
                      price,
                      if (model.contextWindow != null) MetaPill(label: t.contextWindow(formatCount(model.contextWindow!))),
                      if (model.tools == true) MetaPill(label: t.tools, icon: Icons.build_outlined),
                      if (model.vision == true) MetaPill(label: t.vision, icon: Icons.image_outlined),
                      if (model.reasoning == true) MetaPill(label: t.reasoning, icon: Icons.psychology_outlined),
                      if (!model.available)
                        MetaPill(
                          label: model.unavailableUntil == null
                              ? routingReasonLabel(t, model.unavailableReason)
                              : t.unavailableUntil(routingReasonLabel(t, model.unavailableReason), formatTime(DateTime.fromMillisecondsSinceEpoch(model.unavailableUntil!))),
                          tone: PillTone.error,
                        ),
                      if (model.penalty >= 1) MetaPill(label: t.penaltyLabel(model.penalty.toStringAsFixed(1)), tone: PillTone.warning),
                    ],
                  ),
                  if (!wide) ...<Widget>[const SizedBox(height: 8), meters],
                ],
              ),
            ),
            if (wide) ...<Widget>[meters, const SizedBox(width: AppSpacing.md)],
            Switch(value: model.enabled, onChanged: onToggle),
          ],
        ),
      ),
    );
  }
}

class _ModelDialog extends StatefulWidget {
  const _ModelDialog({required this.controller, required this.model});

  final NeoRouterController controller;
  final CatalogModel model;

  @override
  State<_ModelDialog> createState() => _ModelDialogState();
}

class _ModelDialogState extends State<_ModelDialog> {
  late String _free = switch (widget.model.overrides['free']) { true => 'free', false => 'paid', _ => 'auto' };
  late final TextEditingController _priceIn = TextEditingController(text: '${widget.model.overrides['priceIn'] ?? ''}');
  late final TextEditingController _priceOut = TextEditingController(text: '${widget.model.overrides['priceOut'] ?? ''}');
  late final TextEditingController _intelligence = TextEditingController(text: '${widget.model.overrides['intelligence'] ?? ''}');
  String? _error;

  @override
  void dispose() {
    _priceIn.dispose();
    _priceOut.dispose();
    _intelligence.dispose();
    super.dispose();
  }

  double? _number(TextEditingController controller) => double.tryParse(controller.text.trim().replaceAll(',', '.'));

  Future<void> _save() async {
    try {
      final updated = await widget.controller.api.updateModel(widget.model.ref, overrides: <String, Object?>{
        'free': _free == 'auto' ? null : _free == 'free',
        'priceIn': _number(_priceIn),
        'priceOut': _number(_priceOut),
        'intelligence': _number(_intelligence),
      });
      if (mounted) Navigator.of(context).pop(updated);
    } on ApiException catch (error) {
      setState(() => _error = error.message);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final model = widget.model;
    return AlertDialog(
      title: Text(model.name),
      content: SizedBox(
        width: 480,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              SelectableText(model.id, style: TextStyle(fontFamily: 'monospace', color: palette.textSoft)),
              const SizedBox(height: AppSpacing.sm),
              Text(freeReasonLabel(t, model.freeReason), style: TextStyle(color: model.isFree ? palette.success : palette.textSoft)),
              const SizedBox(height: 4),
              Text(
                model.samples == 0 ? t.notMeasured : t.measured(formatMs(model.latencyMs), model.tps?.toStringAsFixed(0) ?? '—', '${model.samples}'),
                style: TextStyle(color: palette.textMuted, fontSize: 12),
              ),
              if (model.intelligenceSource != null)
                Text(t.intelligenceSource(model.intelligenceSource!), style: TextStyle(color: palette.textMuted, fontSize: 12)),
              if (model.lastError != null) ...<Widget>[
                const SizedBox(height: 6),
                Text(model.lastError!, maxLines: 3, overflow: TextOverflow.ellipsis, style: TextStyle(color: palette.warning, fontSize: 12)),
              ],
              const SizedBox(height: AppSpacing.md),
              Text(t.modelSettings.toUpperCase(), style: sectionEyebrowStyle(palette)),
              const SizedBox(height: AppSpacing.sm),
              SegmentedButton<String>(
                segments: <ButtonSegment<String>>[
                  ButtonSegment<String>(value: 'auto', label: Text(t.overrideAuto)),
                  ButtonSegment<String>(value: 'free', label: Text(t.overrideForceFree)),
                  ButtonSegment<String>(value: 'paid', label: Text(t.overrideForcePaid)),
                ],
                selected: <String>{_free},
                onSelectionChanged: (value) => setState(() => _free = value.first),
              ),
              const SizedBox(height: AppSpacing.sm),
              Row(
                children: <Widget>[
                  Expanded(child: TextField(controller: _priceIn, decoration: InputDecoration(labelText: t.inputPrice, hintText: formatPricePerMillion(model.priceIn)))),
                  const SizedBox(width: AppSpacing.sm),
                  Expanded(child: TextField(controller: _priceOut, decoration: InputDecoration(labelText: t.outputPrice, hintText: formatPricePerMillion(model.priceOut)))),
                ],
              ),
              const SizedBox(height: AppSpacing.sm),
              TextField(controller: _intelligence, decoration: InputDecoration(labelText: t.intelligenceOverride, hintText: model.intelligence?.toStringAsFixed(2))),
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: <Widget>[
        TextButton(
          onPressed: () async {
            final updated = await widget.controller.api.resetModelHealth(model.ref);
            if (context.mounted) {
              showSnack(context, t.healthReset);
              Navigator.of(context).pop(updated);
            }
          },
          child: Text(t.resetHealth),
        ),
        OutlinedButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(onPressed: _save, child: Text(t.save)),
      ],
    );
  }
}
