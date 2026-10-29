import 'dart:ui' show ImageFilter;

import 'package:flutter/material.dart';
import 'package:google_fonts/google_fonts.dart';

import 'l10n/gen/app_localizations.dart';
import 'main_controller.dart';
import 'main_shared.dart';
import 'main_spacing.dart';
import 'main_theme.dart';
import 'src/backend_client.dart';
import 'src/capabilities.dart';
import 'src/models.dart';

enum _Filter { all, free, issues }

const int _pageSize = 60;

String freeReasonLabel(AppL10n t, String? reason) => switch (reason) {
  'manual_free' => t.freeReasonManualFree,
  'manual_paid' => t.freeReasonManualPaid,
  'provider_free_model' => t.freeReasonProviderFreeModel,
  'listed_zero_price' => t.freeReasonListedZeroPrice,
  'free_tier_account' => t.freeReasonFreeTierAccount,
  'price_unknown' => t.freeReasonPriceUnknown,
  'billed_observed' => t.freeReasonBilledObserved,
  'no_free_tier' => t.freeReasonNoFreeTier,
  'not_in_free_tier' => t.freeReasonNotInFreeTier,
  'billing_active' => t.freeReasonBillingActive,
  'billing_unverified' => t.freeReasonBillingUnverified,
  _ => t.freeReasonPaid,
};

String routingReasonLabel(AppL10n t, String? reason) {
  final value = reason ?? '';
  if (value.startsWith('quota_')) return t.reasonQuota;
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
    _ => value.replaceAll('_', ' '),
  };
}

String unitLabel(AppL10n t, String unit) => switch (unit) {
  'image' => t.unitImage,
  'second' => t.unitSecond,
  'minute' => t.unitMinute,
  'search' => t.unitSearch,
  _ => t.unitMchar,
};

/// The unit a media kind is usually billed in.
String defaultUnitFor(String capability) => switch (capability) {
  'image' => 'image',
  'video' => 'second',
  'transcription' => 'minute',
  'search' => 'search',
  _ => 'mchar',
};

const Set<String> kUnitPricedKinds = <String>{'image', 'video', 'speech', 'transcription', 'search'};

/// "Free", "$0.04 per image" or "$0.15 / $0.60 per 1M tokens".
String modelPriceLabel(AppL10n t, CatalogModel model) {
  if (model.isFree) return t.free;
  if (model.priceUnit != null && model.unitPrice != null) return t.unitPriceLabel(formatUsd(model.unitPrice!), unitLabel(t, model.priceUnit!));
  if (model.priceIn == null) return t.priceUnknown;
  return t.pricePerMillion(formatPricePerMillion(model.priceIn), formatPricePerMillion(model.priceOut));
}

class ModelsScreen extends StatefulWidget {
  const ModelsScreen({super.key, required this.controller});

  final NeoRouterController controller;

  @override
  State<ModelsScreen> createState() => _ModelsScreenState();
}

class _ModelsScreenState extends State<ModelsScreen> {
  List<CapabilitySummary> _capabilities = <CapabilitySummary>[];
  String _capability = 'chat';
  List<CatalogModel> _models = <CatalogModel>[];
  bool _loading = true;
  String? _error;
  String _query = '';
  _Filter _filter = _Filter.all;
  String? _provider;
  int _shown = _pageSize;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() => _error = null);
    try {
      final api = widget.controller.api;
      final results = await Future.wait(<Future<Object>>[api.capabilities(), api.models(capability: _capability)]);
      setState(() {
        _capabilities = results[0] as List<CapabilitySummary>;
        _models = results[1] as List<CatalogModel>;
      });
    } catch (error) {
      setState(() => _error = '$error');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _selectCapability(String id) {
    setState(() {
      _capability = id;
      _provider = null;
      _filter = _Filter.all;
      _shown = _pageSize;
      _loading = true;
    });
    _load();
  }

  Iterable<CatalogModel> get _visible {
    final query = _query.toLowerCase();
    return _models.where((model) {
      if (_provider != null && model.providerSlug != _provider) return false;
      if (query.isNotEmpty && !model.id.toLowerCase().contains(query) && !model.name.toLowerCase().contains(query)) return false;
      return switch (_filter) {
        _Filter.all => true,
        _Filter.free => model.isFree,
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
    final palette = neoRouterPaletteOf(context);
    final providers = _models.map((model) => model.providerSlug).toSet().toList()..sort();
    final visible = _visible.toList();
    final current = _capabilities.where((item) => item.id == _capability).firstOrNull;
    return PageScaffold(
      header: ScreenHeader(
        title: t.modelsTitle,
        description: t.modelsDescription,
        trailing: PopupMenuButton<String>(
          tooltip: t.moreActions,
          icon: const Icon(Icons.more_horiz_rounded),
          onSelected: (value) => switch (value) {
            'enable' => _bulk(enabled: true),
            'disable' => _bulk(enabled: false),
            _ => _bulk(enabled: true, onlyFree: true),
          },
          itemBuilder: (_) => <PopupMenuEntry<String>>[
            PopupMenuItem<String>(value: 'free', child: Text(t.enableFreeOnly)),
            PopupMenuItem<String>(value: 'enable', child: Text(t.enableAll)),
            PopupMenuItem<String>(value: 'disable', child: Text(t.disableAll)),
          ],
        ),
      ),
      children: <Widget>[
        if (_capabilities.length > 1)
          PageTabs<String>(
            tabs: <({String value, String label, int? count})>[
              for (final item in _capabilities)
                (
                  value: item.id,
                  label: item.routable || item.id == 'router' ? item.info.label(t) : '${item.info.label(t)} · ${t.capSoon}',
                  count: item.routable ? item.total : null,
                ),
            ],
            selected: _capability,
            onChanged: _selectCapability,
          ),
        if (current != null && !current.routable)
          _ComingSoon(capability: current, models: _models, loading: _loading)
        else ...<Widget>[
          Wrap(
            spacing: AppSpacing.sm,
            runSpacing: AppSpacing.sm,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: <Widget>[
              SizedBox(
                width: 300,
                child: TextField(
                  decoration: InputDecoration(prefixIcon: const Icon(Icons.search_rounded, size: 20), hintText: t.searchModels),
                  onChanged: (value) => setState(() {
                    _query = value;
                    _shown = _pageSize;
                  }),
                ),
              ),
              PopupMenuButton<String?>(
                tooltip: t.filterProvider,
                onSelected: (value) => setState(() {
                  _provider = value == '' ? null : value;
                  _shown = _pageSize;
                }),
                itemBuilder: (_) => <PopupMenuEntry<String?>>[
                  PopupMenuItem<String?>(value: '', child: Text(t.allConnections)),
                  for (final provider in providers) PopupMenuItem<String?>(value: provider, child: Text(provider)),
                ],
                child: Container(
                  padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
                  decoration: BoxDecoration(
                    border: Border.all(color: palette.borderStrong),
                    borderRadius: BorderRadius.circular(AppRadius.control),
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: <Widget>[
                      Text(_provider ?? t.allConnections, style: TextStyle(fontSize: 13.5, color: palette.text)),
                      const SizedBox(width: 6),
                      Icon(Icons.expand_more_rounded, size: 18, color: palette.textMuted),
                    ],
                  ),
                ),
              ),
              for (final filter in _Filter.values)
                ChoiceChip(
                  label: Text(switch (filter) {
                    _Filter.all => t.filterAll,
                    _Filter.free => t.filterFree,
                    _Filter.issues => t.filterIssues,
                  }),
                  selected: _filter == filter,
                  onSelected: (_) => setState(() {
                    _filter = filter;
                    _shown = _pageSize;
                  }),
                ),
            ],
          ),
          AsyncBody(
            loading: _loading,
            error: _error,
            onRetry: _load,
            child: visible.isEmpty
                ? AppCard(
                    child: EmptyState(icon: Icons.auto_awesome_outlined, title: t.noModelsTitle, message: t.noModelsMessage),
                  )
                : Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: <Widget>[
                      Padding(
                        padding: const EdgeInsets.fromLTRB(2, 0, 0, 10),
                        child: Text(t.modelsShown('${visible.length}'), style: TextStyle(color: palette.textMuted, fontSize: 12.5)),
                      ),
                      SettingsCardList(
                        children: <Widget>[
                          for (final model in visible.take(_shown))
                            _ModelRow(
                              model: model,
                              routable: current?.routable ?? true,
                              onToggle: (value) => _toggle(model, value),
                              onOpen: () async {
                                final updated = await showDialog<CatalogModel>(
                                  context: context,
                                  builder: (_) => _ModelDialog(controller: widget.controller, model: model),
                                );
                                if (updated != null) await _load();
                              },
                            ),
                        ],
                      ),
                      if (visible.length > _shown)
                        Padding(
                          padding: const EdgeInsets.only(top: AppSpacing.md),
                          child: Center(
                            child: OutlinedButton(
                              onPressed: () => setState(() => _shown += _pageSize * 2),
                              child: Text(t.showMore('${visible.length - _shown}')),
                            ),
                          ),
                        ),
                    ],
                  ),
          ),
        ],
      ],
    );
  }
}

/// A kind NeoRouter lists but cannot route yet: a blurred glimpse of the
/// models already found, under a short explanation.
class _ComingSoon extends StatelessWidget {
  const _ComingSoon({required this.capability, required this.models, required this.loading});

  final CapabilitySummary capability;
  final List<CatalogModel> models;
  final bool loading;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final info = capability.info;
    return SizedBox(
      height: 420,
      child: Stack(
        children: <Widget>[
          Positioned.fill(
            child: IgnorePointer(
              child: ImageFiltered(
                imageFilter: ImageFilter.blur(sigmaX: 7, sigmaY: 7),
                child: Opacity(
                  opacity: 0.55,
                  child: SingleChildScrollView(
                    physics: const NeverScrollableScrollPhysics(),
                    child: SettingsCardList(
                      children: <Widget>[
                        for (final model in models.take(7))
                          ListRow(
                            title: model.name,
                            subtitle: '${model.providerSlug} · ${modelPriceLabel(t, model)}',
                            trailing: Switch(value: model.enabled, onChanged: null),
                          ),
                        if (models.isEmpty && !loading)
                          for (var i = 0; i < 6; i++) ListRow(title: '—', subtitle: '—'),
                      ],
                    ),
                  ),
                ),
              ),
            ),
          ),
          Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: GlassSurface(
                padding: const EdgeInsets.all(24),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: <Widget>[
                    Container(
                      width: 46,
                      height: 46,
                      decoration: BoxDecoration(color: palette.accentSoft, shape: BoxShape.circle),
                      child: Icon(info.icon, size: 22, color: palette.accent),
                    ),
                    const SizedBox(height: 14),
                    Text(
                      capability.id == 'router' ? info.label(t) : t.capComingSoonTitle(info.label(t)),
                      textAlign: TextAlign.center,
                      style: displayTitleStyle(palette, size: 18),
                    ),
                    const SizedBox(height: 8),
                    Text(
                      info.description(t),
                      textAlign: TextAlign.center,
                      style: TextStyle(color: palette.textSoft, height: 1.45),
                    ),
                    if (capability.id != 'router') ...<Widget>[
                      const SizedBox(height: 10),
                      Text(
                        t.capComingSoon('${capability.total}'),
                        textAlign: TextAlign.center,
                        style: TextStyle(color: palette.textMuted, fontSize: 13, height: 1.45),
                      ),
                    ],
                  ],
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

class _ModelRow extends StatelessWidget {
  const _ModelRow({required this.model, required this.routable, required this.onToggle, required this.onOpen});

  final CatalogModel model;
  final bool routable;
  final ValueChanged<bool> onToggle;
  final VoidCallback onOpen;

  @override
  Widget build(BuildContext context) {
    final t = AppL10n.of(context);
    final palette = neoRouterPaletteOf(context);
    final wide = MediaQuery.sizeOf(context).width >= AppBreakpoints.tablet;
    final details = <String>[
      model.providerSlug,
      if (model.contextWindow != null) t.contextWindow(formatCount(model.contextWindow!)),
      modelPriceLabel(t, model),
    ].join(' · ');
    final problem = !model.available
        ? (model.unavailableUntil == null
              ? routingReasonLabel(t, model.unavailableReason)
              : t.unavailableUntil(routingReasonLabel(t, model.unavailableReason), formatTime(DateTime.fromMillisecondsSinceEpoch(model.unavailableUntil!))))
        : null;
    return InkWell(
      onTap: onOpen,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(20, 13, 12, 13),
        child: Row(
          children: <Widget>[
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Row(
                    children: <Widget>[
                      Flexible(
                        child: Text(
                          model.name,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(fontWeight: FontWeight.w500, fontSize: 14, color: model.enabled ? palette.text : palette.textMuted),
                        ),
                      ),
                      for (final (show, icon, label) in <(bool, IconData, String)>[
                        (model.tools == true, Icons.build_outlined, t.tools),
                        (model.vision == true, Icons.image_outlined, t.vision),
                        (model.reasoning == true, Icons.psychology_outlined, t.reasoning),
                      ])
                        if (show)
                          Padding(
                            padding: const EdgeInsets.only(left: 8),
                            child: Tooltip(
                              message: label,
                              child: Icon(icon, size: 14, color: palette.textMuted),
                            ),
                          ),
                    ],
                  ),
                  const SizedBox(height: 3),
                  Text(
                    details,
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: palette.textMuted, fontSize: 12.5),
                  ),
                  if (problem != null) ...<Widget>[
                    const SizedBox(height: 3),
                    Text(
                      problem,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(color: palette.warning, fontSize: 12.5),
                    ),
                  ],
                ],
              ),
            ),
            if (wide && routable) ...<Widget>[
              ScoreMeter(label: t.intelligence, value: model.intelligence),
              const SizedBox(width: 18),
              ScoreMeter(label: t.speed, value: model.speedScore),
              const SizedBox(width: 18),
              ScoreMeter(label: t.reliability, value: model.reliability),
              const SizedBox(width: 20),
            ],
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
  late String _free = switch (widget.model.overrides['free']) {
    true => 'free',
    false => 'paid',
    _ => 'auto',
  };
  late final TextEditingController _priceIn = TextEditingController(text: '${widget.model.overrides['priceIn'] ?? ''}');
  late final TextEditingController _priceOut = TextEditingController(text: '${widget.model.overrides['priceOut'] ?? ''}');
  late final TextEditingController _intelligence = TextEditingController(text: '${widget.model.overrides['intelligence'] ?? ''}');
  late final TextEditingController _unitPrice = TextEditingController(text: '${widget.model.overrides['unitPrice'] ?? ''}');
  late String _unit = '${widget.model.overrides['priceUnit'] ?? widget.model.priceUnit ?? defaultUnitFor(widget.model.capability)}';
  bool _advanced = false;
  String? _error;

  @override
  void dispose() {
    _priceIn.dispose();
    _priceOut.dispose();
    _intelligence.dispose();
    _unitPrice.dispose();
    super.dispose();
  }

  double? _number(TextEditingController controller) => double.tryParse(controller.text.trim().replaceAll(',', '.'));

  Future<void> _save() async {
    try {
      final updated = await widget.controller.api.updateModel(
        widget.model.ref,
        overrides: <String, Object?>{
          'free': _free == 'auto' ? null : _free == 'free',
          'priceIn': _number(_priceIn),
          'priceOut': _number(_priceOut),
          'intelligence': _number(_intelligence),
          if (kUnitPricedKinds.contains(widget.model.capability)) ...<String, Object?>{
            'unitPrice': _number(_unitPrice),
            'priceUnit': _number(_unitPrice) == null ? null : _unit,
          },
        },
      );
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
    final facts = <(String, String)>[
      (t.price, modelPriceLabel(t, model)),
      (t.factWhyFree, freeReasonLabel(t, model.freeReason)),
      if (model.contextWindow != null) (t.factContext, t.contextWindow(formatCount(model.contextWindow!))),
      (t.factMeasured, model.samples == 0 ? t.notMeasured : t.measured(formatMs(model.latencyMs), model.tps?.toStringAsFixed(0) ?? '—', '${model.samples}')),
      if (model.intelligenceSource != null) (t.intelligence, t.intelligenceSource(model.intelligenceSource!)),
    ];
    return AlertDialog(
      title: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Text(model.name),
          const SizedBox(height: 4),
          SelectableText(model.id, style: GoogleFonts.geistMono(fontSize: 12.5, color: palette.textMuted)),
        ],
      ),
      content: SizedBox(
        width: 460,
        child: SingleChildScrollView(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: <Widget>[
              for (final (label, value) in facts)
                Padding(
                  padding: const EdgeInsets.only(bottom: 10),
                  child: Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: <Widget>[
                      SizedBox(
                        width: 130,
                        child: Text(label, style: TextStyle(color: palette.textMuted, fontSize: 13)),
                      ),
                      Expanded(
                        child: Text(value, style: TextStyle(color: palette.text, fontSize: 13, height: 1.4)),
                      ),
                    ],
                  ),
                ),
              if (model.lastError != null) ...<Widget>[const SizedBox(height: 4), InlineHint(message: model.lastError!, icon: Icons.warning_amber_rounded)],
              const SizedBox(height: AppSpacing.md),
              Text(
                t.overrideFree,
                style: TextStyle(fontWeight: FontWeight.w600, color: palette.text),
              ),
              const SizedBox(height: AppSpacing.xs),
              SegmentedButton<String>(
                showSelectedIcon: false,
                segments: <ButtonSegment<String>>[
                  ButtonSegment<String>(value: 'auto', label: Text(t.overrideAuto)),
                  ButtonSegment<String>(value: 'free', label: Text(t.overrideForceFree)),
                  ButtonSegment<String>(value: 'paid', label: Text(t.overrideForcePaid)),
                ],
                selected: <String>{_free},
                onSelectionChanged: (value) => setState(() => _free = value.first),
              ),
              const SizedBox(height: AppSpacing.sm),
              Align(
                alignment: Alignment.centerLeft,
                child: TextButton.icon(
                  onPressed: () => setState(() => _advanced = !_advanced),
                  icon: Icon(_advanced ? Icons.expand_less_rounded : Icons.expand_more_rounded, size: 18),
                  label: Text(t.advancedOptions),
                ),
              ),
              if (_advanced) ...<Widget>[
                Row(
                  children: <Widget>[
                    Expanded(
                      child: TextField(
                        controller: _priceIn,
                        decoration: InputDecoration(labelText: t.inputPrice, hintText: formatPricePerMillion(model.priceIn)),
                      ),
                    ),
                    const SizedBox(width: AppSpacing.sm),
                    Expanded(
                      child: TextField(
                        controller: _priceOut,
                        decoration: InputDecoration(labelText: t.outputPrice, hintText: formatPricePerMillion(model.priceOut)),
                      ),
                    ),
                  ],
                ),
                if (kUnitPricedKinds.contains(model.capability)) ...<Widget>[
                  const SizedBox(height: AppSpacing.sm),
                  Row(
                    children: <Widget>[
                      Expanded(
                        child: TextField(
                          controller: _unitPrice,
                          decoration: InputDecoration(labelText: t.unitPrice, hintText: model.unitPrice == null ? null : formatUsd(model.unitPrice!)),
                        ),
                      ),
                      const SizedBox(width: AppSpacing.sm),
                      DropdownButton<String>(
                        value: _unit,
                        onChanged: (value) => setState(() => _unit = value ?? _unit),
                        items: <DropdownMenuItem<String>>[
                          for (final unit in const <String>['image', 'second', 'minute', 'mchar', 'search'])
                            DropdownMenuItem<String>(value: unit, child: Text(unitLabel(t, unit))),
                        ],
                      ),
                    ],
                  ),
                ],
                const SizedBox(height: AppSpacing.sm),
                TextField(
                  controller: _intelligence,
                  decoration: InputDecoration(labelText: t.intelligenceOverride, hintText: model.intelligence?.toStringAsFixed(2)),
                ),
                const SizedBox(height: AppSpacing.xs),
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton(
                    onPressed: () async {
                      final updated = await widget.controller.api.resetModelHealth(model.ref);
                      if (context.mounted) {
                        showSnack(context, t.healthReset);
                        Navigator.of(context).pop(updated);
                      }
                    },
                    child: Text(t.resetHealth),
                  ),
                ),
              ],
              if (_error != null) ...<Widget>[const SizedBox(height: AppSpacing.sm), InlineError(message: _error!)],
            ],
          ),
        ),
      ),
      actions: <Widget>[
        TextButton(onPressed: () => Navigator.of(context).pop(), child: Text(t.cancel)),
        FilledButton(onPressed: _save, child: Text(t.save)),
      ],
    );
  }
}
